package cmd

import (
	"bufio"
	"cloud-computer-keepalive/internal/config"
	"cloud-computer-keepalive/internal/crypto"
	"cloud-computer-keepalive/internal/diagnostics"
	"cloud-computer-keepalive/internal/logger"
	"cloud-computer-keepalive/internal/soho"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"
)

func RunDaemon() {
	session, diagErr := diagnostics.Start("")
	if diagErr != nil {
		logger.Warnf("Diagnostic logging unavailable: %v", diagErr)
	} else {
		defer diagnostics.Close()
		if err := logger.ConfigureFile(session.RuntimeLog, logger.DEBUG); err != nil {
			logger.Warnf("Runtime file logging unavailable: %v", err)
		} else {
			defer logger.CloseFile()
		}
		logger.Infof("Diagnostic session directory: %s", session.Dir)
		logger.Warn("Diagnostic logs may contain private network and protocol data; do not publish them")
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			path := diagnostics.Incident(fmt.Errorf("panic: %v", recovered), map[string]any{"panic": true})
			logger.Errorf("Fatal panic captured in incident: %s", path)
			panic(recovered)
		}
	}()
	scanner := bufio.NewScanner(os.Stdin)
	cfg, err := config.LoadConfig()
	if err != nil {
		logger.Errorf("Load config failed: %v", err)
		os.Exit(1)
	}
	if err := ensureLocalConfig(scanner, cfg, false); err != nil {
		logger.Errorf("Initial setup failed: %v", err)
		os.Exit(1)
	}

	failures := make(map[failureKind]int)
	for {
		started := time.Now()
		err := RunKeepalive(0)
		if err == nil {
			return
		}
		if time.Since(started) > 10*time.Minute {
			clear(failures)
		}
		decision := classifyKeepaliveError(err)
		failures[decision.kind]++
		incidentPath := diagnostics.Incident(err, map[string]any{
			"failureKind": decision.kind,
			"consecutive": failures[decision.kind],
			"runSeconds":  int(time.Since(started).Seconds()),
		})
		logger.Warnf("Keepalive stopped: kind=%s consecutive=%d reason=%s error=%v",
			decision.kind, failures[decision.kind], decision.description, err)
		if incidentPath != "" {
			logger.Warnf("Incident snapshot saved: %s", incidentPath)
		}
		if decision.fatal {
			logger.Errorf("Automatic retry stopped: %s", decision.description)
			return
		}

		if decision.relogin {
			diagnostics.Event("login_refresh_started", map[string]any{"failureKind": decision.kind})
			if loginErr := ensureLocalConfig(scanner, cfg, true); loginErr != nil {
				loginDecision := classifyKeepaliveError(loginErr)
				failures[loginDecision.kind]++
				logger.Warnf("Refresh login failed: kind=%s error=%v", loginDecision.kind, loginErr)
				decision = loginDecision
				if decision.fatal {
					return
				}
			} else {
				failures[failureAuth] = 0
				diagnostics.Event("login_refresh_succeeded", map[string]any{})
			}
		}

		delay := retryDelay(decision, failures[decision.kind])
		diagnostics.Event("retry_scheduled", map[string]any{
			"failureKind":  decision.kind,
			"consecutive":  failures[decision.kind],
			"delaySeconds": delay.Seconds(),
			"relogin":      decision.relogin,
		})
		logger.Infof("Retrying in %s (kind=%s)...", delay.Round(time.Second), decision.kind)
		if !waitForRetry(delay) {
			logger.Info("User interrupted during retry wait")
			return
		}
	}
}

func waitForRetry(delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	select {
	case <-timer.C:
		return true
	case <-sigCh:
		return false
	}
}

func ensureLocalConfig(scanner *bufio.Scanner, cfg *config.Config, forceLogin bool) error {
	changed := false
	if cfg.DeviceID == "" {
		cfg.DeviceID = config.GenerateDeviceID()
		changed = true
	}

	if cfg.SubAccount == "" {
		cfg.SubAccount = promptLine(scanner, "Sub account")
		changed = true
	}
	if cfg.SubAccount == "" {
		return fmt.Errorf("sub account cannot be empty")
	}

	password := ""
	var passErr error
	if cfg.SubPassword != "" && cfg.SubPasswordBox == nil {
		password = cfg.SubPassword
		box, err := config.EncryptLocalSecret(password)
		if err != nil {
			return fmt.Errorf("encrypt existing password: %w", err)
		}
		cfg.SubPasswordBox = box
		cfg.SubPassword = ""
		cfg.Password = ""
		changed = true
	} else {
		password, passErr = config.DecryptLocalSecret(cfg.SubPasswordBox)
	}
	if cfg.Password != "" || cfg.SubPassword != "" {
		cfg.Password = ""
		cfg.SubPassword = ""
		changed = true
	}
	if passErr != nil && (cfg.SohoToken == "" || forceLogin) {
		password = promptPassword("Sub account password")
		if password == "" {
			return fmt.Errorf("sub account password cannot be empty")
		}
		box, err := config.EncryptLocalSecret(password)
		if err != nil {
			return fmt.Errorf("encrypt password: %w", err)
		}
		cfg.SubPasswordBox = box
		cfg.SubPassword = ""
		cfg.Password = ""
		changed = true
	}

	needsLogin := forceLogin || cfg.SohoToken == "" || cfg.UserID == "" || cfg.UserServiceID == "" || cfg.VMID == ""
	if changed {
		if err := config.SaveConfig(cfg); err != nil {
			return fmt.Errorf("save config: %w", err)
		}
		logger.Infof("Config saved to %s", config.ConfigFilePath())
		changed = false
	}
	if needsLogin {
		if password == "" {
			return fmt.Errorf("password is required to refresh login")
		}
		logger.Infof("Logging in as sub account %s...", logger.Mask(cfg.SubAccount, 4))
		data, err := soho.SubAccountPasswordLogin(cfg.SubAccount, password)
		if err != nil {
			return fmt.Errorf("login failed: %w", err)
		}
		cfg.LoginMode = "sub_password"
		cfg.SohoToken, _ = data["sohoToken"].(string)
		cfg.UserID = jsonString(data["userId"])
		if cfg.SohoToken == "" || cfg.UserID == "" {
			return fmt.Errorf("login response missing token or user id")
		}
		if phone, _ := data["mainAccountPhone"].(string); phone != "" {
			cfg.Phone = phone
		}
		if err := fetchLocalCloudPC(scanner, cfg); err != nil {
			return err
		}
		changed = true
		logger.Infof("Login refreshed for user %s", logger.Mask(cfg.UserID, 4))
	}

	if changed {
		if err := config.SaveConfig(cfg); err != nil {
			return fmt.Errorf("save config: %w", err)
		}
		logger.Infof("Config saved to %s", config.ConfigFilePath())
	}
	return nil
}

func fetchLocalCloudPC(scanner *bufio.Scanner, cfg *config.Config) error {
	logger.Info("Getting cloud PC list...")
	encrypted, err := crypto.RSAEncrypt(`{"pageNum":1,"pageSize":100}`)
	if err != nil {
		return fmt.Errorf("encrypt cloud PC list request: %w", err)
	}
	result, err := soho.SohoRequest("/cc/cloudPc/list/v6", encrypted, cfg.SohoToken, cfg.UserID)
	if err != nil {
		return fmt.Errorf("get cloud PC list: %w", err)
	}
	code, _ := result["code"].(float64)
	if code != 2000 {
		return fmt.Errorf("get cloud PC list failed: code=%v msg=%v", result["code"], result["msg"])
	}
	data, _ := result["data"].(map[string]any)
	listRaw, _ := data["list"].([]any)
	if len(listRaw) == 0 {
		return fmt.Errorf("no cloud PC found for this account")
	}

	idx := 0
	selectedExisting := false
	if cfg.UserServiceID != "" {
		for i, item := range listRaw {
			pc, _ := item.(map[string]any)
			if jsonString(pc["userServiceId"]) == cfg.UserServiceID {
				idx = i
				selectedExisting = true
				break
			}
		}
	}
	if len(listRaw) > 1 && !selectedExisting {
		fmt.Printf("Found %d cloud PCs:\n", len(listRaw))
		for i, item := range listRaw {
			pc, _ := item.(map[string]any)
			fmt.Printf("  [%d] %v (%v) - %v\n", i, pc["vmName"], pc["skuSpec"], pc["vmStatusShow"])
		}
		choice := promptLine(scanner, "Select [0]")
		if choice != "" {
			fmt.Sscanf(choice, "%d", &idx)
		}
		if idx < 0 || idx >= len(listRaw) {
			idx = 0
		}
	} else if selectedExisting {
		logger.Infof("Reusing configured cloud PC userServiceId=%s", cfg.UserServiceID)
	}

	selectedPC, _ := listRaw[idx].(map[string]any)
	userServiceID := jsonString(selectedPC["userServiceId"])
	if userServiceID == "" {
		return fmt.Errorf("selected cloud PC missing userServiceId")
	}
	cfg.UserServiceID = userServiceID
	logger.Infof("Cloud PC: %v (userServiceId=%s)", selectedPC["vmName"], userServiceID)

	authJSON := fmt.Sprintf(`{"userServiceId":"%s"}`, userServiceID)
	encrypted, err = crypto.RSAEncrypt(authJSON)
	if err != nil {
		return fmt.Errorf("encrypt firm auth request: %w", err)
	}
	result, err = soho.SohoRequest("/cc/getFirmAuth/v1", encrypted, cfg.SohoToken, cfg.UserID)
	if err != nil {
		return fmt.Errorf("getFirmAuth: %w", err)
	}
	code, _ = result["code"].(float64)
	if code != 2000 {
		return fmt.Errorf("getFirmAuth failed: code=%v msg=%v", result["code"], result["msg"])
	}
	data, _ = result["data"].(map[string]any)
	cfg.VMID = jsonString(data["vmId"])
	if cfg.VMID == "" {
		return fmt.Errorf("getFirmAuth response missing vmId")
	}
	logger.Infof("vmId: %s", logger.Mask(cfg.VMID, 4))
	return nil
}

func promptLine(scanner *bufio.Scanner, label string) string {
	fmt.Printf("%s: ", label)
	scanner.Scan()
	return strings.TrimSpace(scanner.Text())
}

func promptPassword(label string) string {
	fmt.Printf("%s: ", label)
	if term.IsTerminal(int(os.Stdin.Fd())) {
		value, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err == nil {
			return strings.TrimSpace(string(value))
		}
	}
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	return strings.TrimSpace(scanner.Text())
}
