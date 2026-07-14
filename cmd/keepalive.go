package cmd

import (
	"bytes"
	"cloud-computer-keepalive/internal/cem"
	"cloud-computer-keepalive/internal/chuanyun"
	"cloud-computer-keepalive/internal/config"
	"cloud-computer-keepalive/internal/crypto"
	"cloud-computer-keepalive/internal/logger"
	"cloud-computer-keepalive/internal/scg"
	"cloud-computer-keepalive/internal/soho"
	"cloud-computer-keepalive/internal/spice"
	"cloud-computer-keepalive/internal/zte"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"
)

func Keepalive(args []string) {
	duration := 120
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--duration":
			if i+1 < len(args) {
				d, err := strconv.Atoi(args[i+1])
				if err == nil {
					duration = d
				}
				i++
			}
		case "--forever":
			duration = 0
		}
	}
	if err := RunKeepalive(duration); err != nil {
		logger.Errorf("Keepalive failed: %v", err)
		os.Exit(1)
	}
}

func RunKeepalive(duration int) error {
	sohoToken, userID := soho.LoadSohoToken()
	if sohoToken == "" {
		return fmt.Errorf("missing soho token")
	}
	logger.Infof("SohoToken: %s", logger.Mask(sohoToken, 4))

	// 1. Get firm auth. Sub-account/ZTE clients may receive CAG/VMC fields
	// without an SCG auth code, matching the official Windows client flow.
	firmAuth, err := cem.GetFirmAuth(sohoToken, userID)
	if err != nil {
		return fmt.Errorf("getFirmAuth failed: %w", err)
	}
	firmAuthCode, _ := firmAuth["scAuthCode"].(string)
	if firmAuthCode == "" {
		return keepaliveZTE(firmAuth, sohoToken, userID, duration)
	}

	accessToken, err := cem.ExchangeCEMAccessToken(firmAuthCode)
	if err != nil {
		return fmt.Errorf("get CEM access_token failed: %w", err)
	}

	// 2. Call getConnectInfo to trigger VM boot
	logger.Info("Calling getConnectInfo...")
	connectInfo, err := cem.GetConnectInfo(accessToken)
	if err != nil {
		return fmt.Errorf("getConnectInfo failed: %w", err)
	}

	logger.Infof("SCG: %s:%s, readyStatus=%.0f", connectInfo.ScgIP, connectInfo.ScgPort, connectInfo.ReadyStatus)
	logger.Infof("scAuthCode: %d chars", len(connectInfo.ScAuthCode))

	scAuthCode := connectInfo.ScAuthCode

	// 3. Wait for VM ready
	if connectInfo.ReadyStatus != 1 && connectInfo.TraceID != "" {
		logger.Info("Waiting for VM ready...")
		readyInfo, err := cem.WaitVMReady(accessToken, connectInfo.TraceID)
		if err != nil {
			return fmt.Errorf("VM ready timeout: %w", err)
		}
		if readyInfo.ScAuthCode != "" {
			scAuthCode = readyInfo.ScAuthCode
			logger.Infof("Using getVmReadyStatus scAuthCode (%d chars)", len(scAuthCode))
		}
		logger.Info("VM ready")
	}

	// 4. Connect SCG and authenticate
	cfg, _ := config.LoadConfig()
	tlsConn, _, err := scg.ConnectSCG(connectInfo.ScgIP, connectInfo.ScgPort, scAuthCode, cfg.VMID)
	if err != nil {
		return fmt.Errorf("connect SCG failed: %w", err)
	}

	// 5. Keepalive loop
	return keepaliveLoop(tlsConn, sohoToken, userID, duration)
}

func keepaliveZTE(firmAuth map[string]any, sohoToken, userID string, duration int) error {
	return keepaliveZTESession(firmAuth, sohoToken, userID, duration)
}

func keepaliveZTESession(firmAuth map[string]any, sohoToken, userID string, duration int) error {
	logFirmAuthFallback(firmAuth)

	firm := zte.FirmAuth{
		VMUserName: jsonString(firmAuth["vmUserName"]),
		VMPassword: jsonString(firmAuth["vmPassword"]),
		VMID:       jsonString(firmAuth["vmId"]),
		VMCIP:      jsonString(firmAuth["vmcIp"]),
		VMCPort:    mustAtoi(jsonString(firmAuth["vmcPort"])),
		CAGIP:      jsonString(firmAuth["cagIp"]),
		CAGPort:    mustAtoi(jsonString(firmAuth["cagPort"])),
	}
	if firm.VMUserName == "" || firm.VMPassword == "" || firm.VMID == "" || firm.CAGIP == "" || firm.CAGPort == 0 {
		return fmt.Errorf("firm auth is missing required ZTE fields")
	}

	client := zte.NewClient(firm)
	logger.Info("ZTE sysConfig...")
	if _, err := client.SysConfig(); err != nil {
		logger.Warnf("ZTE sysConfig failed, continue: %v", err)
	}

	logger.Info("ZTE getToken...")
	token, err := client.GetAccessToken()
	if err != nil {
		return err
	}
	logger.Infof("ZTE accessToken: %s", logger.Mask(token.AccessToken, 4))

	logger.Info("ZTE getDesktopList...")
	list, err := client.GetDesktopList(token.AccessToken)
	if err != nil {
		return err
	}
	desktop := zte.FirstDesktop(list, firm.VMID)
	if desktop == nil {
		return fmt.Errorf("ZTE desktop list has no matching vmId")
	}

	logger.Info("ZTE startDesktop...")
	start, err := client.StartDesktop(token.AccessToken, desktop)
	if err != nil {
		return err
	}
	connectStr := jsonString(start["connectStr"])
	if connectStr == "" {
		logger.Info("ZTE startDesktop returned no connectStr, querying async result...")
		for i := 0; i < 30; i++ {
			async, err := client.StartDesktopAsyncQuery(token.AccessToken)
			if err != nil {
				return err
			}
			connectStr = jsonString(async["connectStr"])
			if connectStr != "" {
				logger.Infof("ZTE async start ready after %ds", (i+1)*2)
				break
			}
			time.Sleep(2 * time.Second)
		}
	}
	if connectStr == "" {
		return fmt.Errorf("ZTE startDesktop did not return connectStr")
	}

	params, err := zte.DecodeConnectParams(connectStr)
	if err != nil {
		return err
	}
	logger.Infof("ZTE target: spice=%s:%d vmId=%s proxySport=%d", params.Host, params.Port, logger.Mask(params.VMID, 4), params.ProxySport)

	authTemplate := os.Getenv("CCK_ZTE_CAG_AUTH_TEMPLATE_HEX")
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cagAddr := fmt.Sprintf("%s:%d", firm.CAGIP, firm.CAGPort)
	logger.Infof("Connecting ZTE CAG %s...", cagAddr)
	tlsConn, session, err := zte.DialCAGTCPTLS(ctx, zte.CAGDialOptions{
		Address:         cagAddr,
		Params:          params,
		AuthTemplateHex: authTemplate,
		Timeout:         30 * time.Second,
	})
	if err != nil {
		logger.Warnf("ZTE CAG TCP/TLS failed, trying UDP/KCP path: %v", err)
		tlsConn, session, err = zte.DialCAGTLS(ctx, zte.CAGDialOptions{
			Address:         cagAddr,
			Params:          params,
			AuthTemplateHex: authTemplate,
			Timeout:         30 * time.Second,
		})
		if err != nil {
			return err
		}
	}
	defer tlsConn.Close()
	logger.Infof("ZTE CAG TLS established: conv=0x%08x", session.Conv)

	mux := zte.NewCAGMux(tlsConn)
	proxyConn, err := zte.OpenCAGMuxLink(ctx, mux, params, 1)
	if err != nil {
		return err
	}
	logger.Info("ZTE CAG proxy add-link sent")
	rawResult := spice.RawMainHandshake(proxyConn, params.Key, params.VMID, proxyConn.LinkUUID(), proxyConn.TraceID(), proxyConn.REDQSpanID())
	if !rawResult.OK {
		return fmt.Errorf("ZTE raw SPICE main handshake failed")
	}
	subLinks, authed, err := sendZTESubchannelREDQs(ctx, mux, params, proxyConn, rawResult.SpiceSessionID)
	if err != nil {
		logger.Warnf("ZTE subchannel REDQ probe failed: %v", err)
	}
	health := startZTESubchannelKeepalive(subLinks, authed)
	return keepaliveRawSpiceLoop(proxyConn, sohoToken, userID, duration, health)
}

func sendZTESubchannelREDQs(ctx context.Context, mux *zte.CAGMux, params *zte.ConnectParams, main *zte.CAGMuxLink, connectionID uint32) (map[byte]*zte.CAGMuxLink, map[byte]bool, error) {
	type subREDQ struct {
		linkID      byte
		channelType uint8
		channelID   uint8
	}
	firstLinks := []byte{2, 3, 4, 5}
	secondLinks := []byte{6, 7, 8}
	redqs := []subREDQ{
		{linkID: 3, channelType: 4, channelID: 1},
		{linkID: 2, channelType: 6, channelID: 0},
		{linkID: 4, channelType: 5, channelID: 0},
		{linkID: 6, channelType: 3, channelID: 0},
		{linkID: 7, channelType: 2, channelID: 0},
		{linkID: 8, channelType: 4, channelID: 0},
		{linkID: 5, channelType: 2, channelID: 1},
	}

	links := make(map[byte]*zte.CAGMuxLink)
	for _, linkID := range firstLinks {
		link, err := zte.OpenCAGMuxLinkWithTrace(ctx, mux, params, linkID, main.TraceID(), main.SpanID())
		if err != nil {
			return links, nil, err
		}
		links[linkID] = link
	}
	for _, item := range redqs[:3] {
		payload := spice.BuildZTERawChannelREDQ(params.Key, params.VMID, main.LinkUUID(), main.TraceID(), main.REDQSpanID(), connectionID, item.channelType, item.channelID)
		if _, err := links[item.linkID].Write(payload); err != nil {
			return links, nil, err
		}
	}
	for _, linkID := range secondLinks {
		link, err := zte.OpenCAGMuxLinkWithTrace(ctx, mux, params, linkID, main.TraceID(), main.SpanID())
		if err != nil {
			return links, nil, err
		}
		links[linkID] = link
	}
	for _, item := range redqs[3:] {
		payload := spice.BuildZTERawChannelREDQ(params.Key, params.VMID, main.LinkUUID(), main.TraceID(), main.REDQSpanID(), connectionID, item.channelType, item.channelID)
		if _, err := links[item.linkID].Write(payload); err != nil {
			return links, nil, err
		}
	}
	logger.Infof("ZTE subchannel REDQ probe sent (connectionID=0x%s)", logger.Mask(fmt.Sprintf("%08x", connectionID), 4))
	authed := authenticateZTESubchannels(links, 8*time.Second)
	return links, authed, nil
}

func authenticateZTESubchannels(links map[byte]*zte.CAGMuxLink, timeout time.Duration) map[byte]bool {
	pending := make(map[byte][]byte)
	authing := make(map[byte]bool)
	done := make(map[byte]bool)
	deadline := time.Now().Add(timeout)
	defer func() {
		logger.Infof("ZTE subchannel auth completed: %d/%d", len(done), len(links))
	}()
	defer func() {
		for linkID, link := range links {
			if done[linkID] {
				_ = link.SetReadDeadline(time.Time{})
			}
		}
	}()
	for time.Now().Before(deadline) && len(done) < len(links) {
		progress := false
		for linkID, link := range links {
			if done[linkID] {
				continue
			}
			_ = link.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
			payload := make([]byte, 4096)
			n, err := link.Read(payload)
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					continue
				}
				logger.Debugf("ZTE subchannel link=%d read stopped: %v", linkID, err)
				continue
			}
			payload = payload[:n]
			progress = true
			if authing[linkID] && len(payload) == 4 {
				result := binary.LittleEndian.Uint32(payload)
				logger.Debugf("ZTE subchannel link=%d auth result=%d", linkID, result)
				if result == 0 {
					done[linkID] = true
				}
				continue
			}
			pending[linkID] = append(pending[linkID], payload...)
			if !authing[linkID] && len(pending[linkID]) > 32 {
				if !bytes.Contains(pending[linkID], []byte("REDQ")) {
					continue
				}
				ticket := make([]byte, 128)
				if _, err := link.Write(ticket); err != nil {
					return done
				}
				authing[linkID] = true
				logger.Debugf("ZTE subchannel link=%d auth ticket sent", linkID)
			}
		}
		if !progress {
			time.Sleep(20 * time.Millisecond)
		}
	}
	return done
}

type zteDisplayHealth struct {
	initSent bool
	messages uint64
	mark     bool
	surface  bool
	draw     bool
	closed   bool
}

type zteSessionHealth struct {
	mu       sync.Mutex
	displays map[byte]*zteDisplayHealth
	errCh    chan error
}

func newZTESessionHealth(authed map[byte]bool) *zteSessionHealth {
	h := &zteSessionHealth{
		displays: make(map[byte]*zteDisplayHealth),
		errCh:    make(chan error, 1),
	}
	for _, linkID := range []byte{5, 7} {
		if authed[linkID] {
			h.displays[linkID] = &zteDisplayHealth{}
		}
	}
	return h
}

func (h *zteSessionHealth) markDisplayInit(linkID byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if display := h.displays[linkID]; display != nil {
		display.initSent = true
	}
}

func (h *zteSessionHealth) observe(linkID byte, state *spice.RawState) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if display := h.displays[linkID]; display != nil {
		display.messages = state.Messages
		display.mark = state.MarkReceived
		display.surface = state.SurfaceCreated
		display.draw = state.DrawReceived
	}
}

func (h *zteSessionHealth) linkClosed(linkID byte, err error) {
	h.mu.Lock()
	display := h.displays[linkID]
	if display == nil {
		h.mu.Unlock()
		return
	}
	display.closed = true
	allClosed := true
	for _, item := range h.displays {
		if !item.closed {
			allClosed = false
			break
		}
	}
	h.mu.Unlock()
	if allClosed {
		select {
		case h.errCh <- fmt.Errorf("all ZTE display channels closed: %w", err):
		default:
		}
	}
}

func (h *zteSessionHealth) ready() (bool, string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for linkID, display := range h.displays {
		if !display.initSent {
			continue
		}
		if display.mark || display.surface || display.draw {
			return true, fmt.Sprintf("link=%d mark=%v surface=%v draw=%v messages=%d",
				linkID, display.mark, display.surface, display.draw, display.messages)
		}
	}
	return false, ""
}

func (h *zteSessionHealth) displayCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.displays)
}

func startZTESubchannelKeepalive(links map[byte]*zte.CAGMuxLink, authed map[byte]bool) *zteSessionHealth {
	health := newZTESessionHealth(authed)
	for linkID, link := range links {
		if !authed[linkID] {
			continue
		}
		switch linkID {
		case 6:
			if _, err := spice.WriteRawMessage(link, 1, spice.BuildZTERawInputInit()); err != nil {
				logger.Warnf("ZTE input init failed on link=%d: %v", linkID, err)
			} else {
				logger.Debugf("ZTE input init sent on link=%d", linkID)
			}
		case 5, 7:
			if _, err := spice.WriteRawMessage(link, 1, spice.BuildZTERawDisplayInit()); err != nil {
				logger.Warnf("ZTE display init failed on link=%d: %v", linkID, err)
				health.linkClosed(linkID, err)
			} else {
				health.markDisplayInit(linkID)
				logger.Debugf("ZTE display init sent on link=%d", linkID)
			}
		}
		go keepZTESubchannelAlive(linkID, link, health)
	}
	return health
}

func keepZTESubchannelAlive(linkID byte, link *zte.CAGMuxLink, health *zteSessionHealth) {
	state := &spice.RawState{}
	for {
		msgType, payload, err := state.ReadMessage(link, 30*time.Second)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			if err != io.EOF {
				logger.Debugf("ZTE subchannel link=%d stopped: %v", linkID, err)
			}
			health.linkClosed(linkID, err)
			return
		}
		logger.Debugf("ZTE subchannel link=%d recv msg=0x%02x len=%d", linkID, msgType, len(payload))
		replied, replyErr := state.HandleMessage(link, msgType, payload)
		health.observe(linkID, state)
		if replyErr != nil {
			logger.Debugf("ZTE subchannel link=%d reply failed: %v", linkID, replyErr)
			health.linkClosed(linkID, replyErr)
			return
		}
		if replied {
			logger.Debugf("ZTE subchannel link=%d auto-reply msg=0x%02x", linkID, msgType)
		}
	}
}

func keepaliveRawSpiceLoop(conn net.Conn, sohoToken, userID string, duration int, health *zteSessionHealth) error {
	if duration > 0 {
		logger.Infof("Keeping raw SPICE connection for %ds...", duration)
	} else {
		logger.Info("Persistent raw SPICE keepalive (Ctrl+C to exit)...")
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	start := time.Now()
	heartbeatCount := 0
	heartbeatFailures := 0
	rawState := &spice.RawState{}
	nextHeartbeat := start
	readinessDeadline := start.Add(30 * time.Second)
	displayReady := false
	if health == nil || health.displayCount() == 0 {
		return fmt.Errorf("ZTE session has no authenticated display channel")
	}
	logger.Info("Waiting for ZTE display surface evidence...")

	defer func() {
		conn.Close()
		logger.Info("Raw SPICE connection closed")
	}()

	for {
		select {
		case <-sigCh:
			logger.Info("User interrupted")
			return nil
		default:
		}

		elapsed := int(time.Since(start).Seconds())
		if duration > 0 && elapsed >= duration {
			logger.Infof("Raw SPICE keepalive %ds done, disconnecting", elapsed)
			return nil
		}

		now := time.Now()
		if !displayReady {
			if ready, evidence := health.ready(); ready {
				displayReady = true
				logger.Infof("ZTE display session ready: %s", evidence)
			} else if now.After(readinessDeadline) {
				return fmt.Errorf("ZTE display readiness timeout: no MARK, SURFACE_CREATE, or DRAW_COPY after DISPLAY_INIT")
			}
		}
		select {
		case healthErr := <-health.errCh:
			return healthErr
		default:
		}
		if !now.Before(nextHeartbeat) {
			if err := sendSohoHeartbeat(sohoToken, userID); err != nil {
				heartbeatFailures++
				logger.Warnf("Heartbeat error (%d/3): %v", heartbeatFailures, err)
				if heartbeatFailures >= 3 {
					return fmt.Errorf("SOHO heartbeat failed %d consecutive times: %w", heartbeatFailures, err)
				}
			} else {
				heartbeatFailures = 0
			}
			heartbeatCount++
			logger.Infof("Raw SPICE heartbeat #%d (uptime=%ds)", heartbeatCount, elapsed)
			nextHeartbeat = now.Add(25 * time.Second)
		}

		msgType, payload, err := rawState.ReadMessage(conn, 1*time.Second)
		if err != nil {
			if err == io.EOF {
				logger.Info("Raw SPICE server closed connection")
				return fmt.Errorf("raw SPICE server closed connection")
			}
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			return fmt.Errorf("raw SPICE main read failed: %w", err)
		}
		logger.Debugf("Raw SPICE loop recv msg=0x%02x len=%d", msgType, len(payload))
		replied, replyErr := rawState.HandleMessage(conn, msgType, payload)
		if replyErr != nil {
			return replyErr
		}
		if replied {
			logger.Debugf("Raw SPICE auto-reply msg=0x%02x", msgType)
		}
	}
}

func keepaliveLoop(conn net.Conn, sohoToken, userID string, duration int) error {
	if duration > 0 {
		logger.Infof("Keeping connection for %ds...", duration)
	} else {
		logger.Info("Persistent keepalive (Ctrl+C to exit)...")
	}

	// SPICE handshake
	result := spice.SpiceHandshake(conn)
	logger.Infof("Handshake result: spice=%v displayReady=%v channels=%v", result.SpiceOK, result.DisplayReady, result.ConnectedChannels)
	logger.Debugf("sid=0x%x spice_sid=0x%x", result.SessionID, result.SpiceSessionID)
	if !result.SpiceOK || !result.DisplayReady {
		return fmt.Errorf("SCG SPICE display session did not reach MARK/Surface readiness")
	}

	sid := result.SessionID

	// Signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	start := time.Now()
	heartbeatCount := 0
	heartbeatFailures := 0
	ackStates := make(map[uint64]*scgAckState)
	nextHeartbeat := start

	defer func() {
		conn.Close()
		logger.Info("Connection closed")
	}()

	for {
		select {
		case <-sigCh:
			logger.Info("User interrupted")
			return nil
		default:
		}

		elapsed := int(time.Since(start).Seconds())
		if duration > 0 && elapsed >= duration {
			logger.Infof("Keepalive %ds done, disconnecting", elapsed)
			return nil
		}

		now := time.Now()
		if !now.Before(nextHeartbeat) {
			if err := sendSohoHeartbeat(sohoToken, userID); err != nil {
				heartbeatFailures++
				logger.Warnf("Heartbeat error (%d/3): %v", heartbeatFailures, err)
				if heartbeatFailures >= 3 {
					return fmt.Errorf("SOHO heartbeat failed %d consecutive times: %w", heartbeatFailures, err)
				}
			} else {
				heartbeatFailures = 0
			}
			heartbeatCount++

			mouseMode := make([]byte, 10)
			binary.LittleEndian.PutUint16(mouseMode[0:2], 0x69)
			binary.LittleEndian.PutUint32(mouseMode[2:6], 4)
			binary.LittleEndian.PutUint32(mouseMode[6:10], 2)
			head := chuanyun.FrameHeadPack(spice.DataType, uint16(len(mouseMode)), sid, 1)
			if _, err := conn.Write(append(head, mouseMode...)); err != nil {
				return fmt.Errorf("send SCG mouse mode keepalive: %w", err)
			}

			logger.Infof("Heartbeat #%d (uptime=%ds, spice=%v, channels=%v)",
				heartbeatCount, elapsed, result.SpiceOK, result.ConnectedChannels)
			nextHeartbeat = now.Add(25 * time.Second)
		}

		frame, err := chuanyun.RecvTrunkFrame(conn, time.Second)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			return fmt.Errorf("SCG connection read failed: %w", err)
		}
		if err := handleSCGFrame(conn, sid, frame, ackStates); err != nil {
			return err
		}
	}
}

type scgAckState struct {
	window  uint32
	pending uint32
}

func handleSCGFrame(conn net.Conn, sid uint64, frame *chuanyun.Frame, ackStates map[uint64]*scgAckState) error {
	if frame.PktType == chuanyun.TrunkSwitch && len(frame.Payload) >= 32 {
		senderCID := binary.LittleEndian.Uint64(frame.Payload[8:16])
		param := binary.LittleEndian.Uint32(frame.Payload[16:20])
		switchReason := frame.Payload[20]
		extraID := binary.LittleEndian.Uint64(frame.Payload[24:32])
		resp := chuanyun.TrunkSwitchPack(senderCID, sid, param, switchReason, extraID, frame.Field1, frame.Field2)
		if _, err := conn.Write(resp); err != nil {
			return fmt.Errorf("send SCG trunk switch reply: %w", err)
		}
		logger.Debugf("Switch reply: reason=%d", switchReason)
		return nil
	}
	if frame.PktType != spice.DataType || len(frame.Payload) < 6 {
		return nil
	}

	msgType := binary.LittleEndian.Uint16(frame.Payload[0:2])
	chName := chuanyun.ChannelNames[frame.Field2]
	if chName == "" {
		chName = fmt.Sprintf("ch%d", frame.Field2)
	}
	state := ackStates[frame.Field2]
	if state == nil {
		state = &scgAckState{}
		ackStates[frame.Field2] = state
	}

	switch msgType {
	case 0x04:
		pingData := frame.Payload[6:]
		pong := make([]byte, 6+len(pingData))
		binary.LittleEndian.PutUint16(pong[0:2], 0x03)
		binary.LittleEndian.PutUint32(pong[2:6], uint32(len(pingData)))
		copy(pong[6:], pingData)
		head := chuanyun.FrameHeadPack(spice.DataType, uint16(len(pong)), sid, frame.Field2)
		if _, err := conn.Write(append(head, pong...)); err != nil {
			return fmt.Errorf("send SCG PONG on %s: %w", chName, err)
		}
		logger.Debugf("PING -> PONG (ch=%s)", chName)
	case 0x03:
		var generation uint32
		if len(frame.Payload) >= 10 {
			generation = binary.LittleEndian.Uint32(frame.Payload[6:10])
		}
		if len(frame.Payload) >= 14 {
			state.window = binary.LittleEndian.Uint32(frame.Payload[10:14])
		}
		state.pending = 0
		ackSync := make([]byte, 10)
		binary.LittleEndian.PutUint16(ackSync[0:2], 0x01)
		binary.LittleEndian.PutUint32(ackSync[2:6], 4)
		binary.LittleEndian.PutUint32(ackSync[6:10], generation)
		head := chuanyun.FrameHeadPack(spice.DataType, uint16(len(ackSync)), sid, frame.Field2)
		if _, err := conn.Write(append(head, ackSync...)); err != nil {
			return fmt.Errorf("send SCG ACK_SYNC on %s: %w", chName, err)
		}
		logger.Debugf("SET_ACK -> ACK_SYNC (gen=%d, window=%d, ch=%s)", generation, state.window, chName)
		return nil
	}

	if state.window > 0 {
		state.pending++
		if state.pending >= state.window {
			ack := make([]byte, 6)
			binary.LittleEndian.PutUint16(ack[0:2], 0x02)
			head := chuanyun.FrameHeadPack(spice.DataType, uint16(len(ack)), sid, frame.Field2)
			if _, err := conn.Write(append(head, ack...)); err != nil {
				return fmt.Errorf("send SCG ACK on %s: %w", chName, err)
			}
			state.pending = 0
			logger.Debugf("ACK sent (window=%d, ch=%s)", state.window, chName)
		}
	}
	return nil
}

func sendSohoCloudPCAction(path, sohoToken, userID string) error {
	cfg, err := config.LoadConfig()
	if err != nil {
		return err
	}
	if cfg.UserServiceID == "" {
		return fmt.Errorf("missing user_service_id in config, please run login first")
	}

	bodyJSON := fmt.Sprintf(`{"userServiceId":"%s"}`, cfg.UserServiceID)
	bodyData, err := crypto.RSAEncrypt(bodyJSON)
	if err != nil {
		return err
	}

	result, err := soho.SohoRequest(path, bodyData, sohoToken, userID)
	if err != nil {
		return err
	}
	code, _ := result["code"].(float64)
	if code != 2000 && code != 4041 {
		return fmt.Errorf("code=%v, msg=%v", result["code"], result["msg"])
	}
	return nil
}

func sendSohoHeartbeat(sohoToken, userID string) error {
	cfg, err := config.LoadConfig()
	if err != nil {
		return fmt.Errorf("load config for heartbeat: %w", err)
	}
	if cfg.UserServiceID == "" {
		return fmt.Errorf("missing user_service_id for heartbeat")
	}
	bodyJSON := fmt.Sprintf(`{"userServiceId":"%s"}`, cfg.UserServiceID)
	bodyData, err := crypto.RSAEncrypt(bodyJSON)
	if err != nil {
		return fmt.Errorf("encrypt heartbeat: %w", err)
	}
	result, err := soho.SohoRequest("/cc/cloudPc/heartbeat/v2", bodyData, sohoToken, userID)
	if err != nil {
		return err
	}
	code := jsonString(result["code"])
	if code != "2000" && code != "4041" {
		return fmt.Errorf("heartbeat rejected: code=%s msg=%v", code, result["msg"])
	}
	return nil
}

func logFirmAuthFallback(data map[string]any) {
	vmc := jsonString(data["vmcIp"])
	vmcPort := jsonString(data["vmcPort"])
	cag := jsonString(data["cagIp"])
	cagPort := jsonString(data["cagPort"])
	vmID := jsonString(data["vmId"])
	if vmc != "" || cag != "" {
		logger.Infof("Firm auth: vmId=%s, vmc=%s:%s, cag=%s:%s",
			logger.Mask(vmID, 4), vmc, vmcPort, cag, cagPort)
	}
}

func mustAtoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
