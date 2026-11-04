package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"go.mau.fi/whatsmeow"
)

// Pairing over the local API, for installs with no terminal (the Claude
// Desktop extension). WhatsApp gives each login attempt a ~160 second window;
// a new window is opened whenever someone asks for a code and none is open.

const pairWindow = 150 * time.Second

type pairManager struct {
	client *whatsmeow.Client

	mu          sync.Mutex
	state       string // "idle", "waiting", "error"
	qr          string
	errMsg      string
	windowStart time.Time
	gotCode     chan struct{}
}

func newPairManager(client *whatsmeow.Client) *pairManager {
	return &pairManager{client: client, state: "idle"}
}

func (p *pairManager) paired() bool { return p.client.Store.ID != nil }

// openWindow starts a login attempt unless one is still fresh, then waits for
// WhatsApp's first QR code so a phone pairing code can be requested right away.
func (p *pairManager) openWindow() error {
	p.mu.Lock()
	if p.state == "waiting" && time.Since(p.windowStart) < pairWindow {
		ch := p.gotCode
		p.mu.Unlock()
		return waitCode(ch)
	}
	if p.client.IsConnected() {
		p.client.Disconnect()
	}
	qrChan, err := p.client.GetQRChannel(context.Background())
	if err != nil {
		p.mu.Unlock()
		return fmt.Errorf("could not start pairing: %v", err)
	}
	if err := p.client.Connect(); err != nil {
		p.mu.Unlock()
		return fmt.Errorf("could not reach WhatsApp: %v", err)
	}
	p.state, p.qr, p.errMsg = "waiting", "", ""
	p.windowStart = time.Now()
	p.gotCode = make(chan struct{})
	ch := p.gotCode
	p.mu.Unlock()

	go p.consume(qrChan, ch)
	return waitCode(ch)
}

func waitCode(ch chan struct{}) error {
	select {
	case <-ch:
		return nil
	case <-time.After(15 * time.Second):
		return fmt.Errorf("WhatsApp did not respond in time; try again")
	}
}

func (p *pairManager) consume(qrChan <-chan whatsmeow.QRChannelItem, gotCode chan struct{}) {
	signalled := false
	signal := func() {
		if !signalled {
			close(gotCode)
			signalled = true
		}
	}
	for evt := range qrChan {
		p.mu.Lock()
		switch evt.Event {
		case "code":
			p.qr = evt.Code
		case "success":
			p.state = "idle"
		case "timeout":
			p.state = "idle"
		case "err-client-outdated":
			p.state, p.errMsg = "error", "this version is outdated and WhatsApp refused it; the extension needs an update"
		default:
			p.state = "error"
			p.errMsg = evt.Event
			if evt.Error != nil {
				p.errMsg = evt.Error.Error()
			}
		}
		p.mu.Unlock()
		signal()
	}
	signal()
}

type PairResponse struct {
	State     string `json:"state"`
	Number    string `json:"number,omitempty"`
	QR        string `json:"qr,omitempty"`
	Code      string `json:"code,omitempty"`
	ExpiresIn int    `json:"expires_in,omitempty"`
	Message   string `json:"message,omitempty"`
}

func (p *pairManager) current() PairResponse {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.state == "error" {
		return PairResponse{State: "error", Message: p.errMsg}
	}
	left := int((pairWindow - time.Since(p.windowStart)).Seconds())
	if left < 0 {
		left = 0
	}
	return PairResponse{State: "waiting", QR: p.qr, ExpiresIn: left}
}

func (p *pairManager) register(mux *http.ServeMux) {
	mux.HandleFunc("/api/pair", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if p.paired() {
			writeJSON(w, http.StatusOK, PairResponse{State: "paired", Number: p.client.Store.ID.User})
			return
		}
		if err := p.openWindow(); err != nil {
			writeJSON(w, http.StatusBadGateway, PairResponse{State: "error", Message: err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, p.current())
	})

	mux.HandleFunc("/api/pair/phone", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Phone string `json:"phone"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request format", http.StatusBadRequest)
			return
		}
		if p.paired() {
			writeJSON(w, http.StatusOK, PairResponse{State: "paired", Number: p.client.Store.ID.User})
			return
		}
		norm, err := normaliseRecipient(req.Phone)
		if err != nil || strings.Contains(req.Phone, "@") {
			writeJSON(w, http.StatusBadRequest, PairResponse{State: "error", Message: "phone must be your number with country code, e.g. 84901234567"})
			return
		}
		phone := strings.TrimSuffix(norm, "@s.whatsapp.net")
		if err := p.openWindow(); err != nil {
			writeJSON(w, http.StatusBadGateway, PairResponse{State: "error", Message: err.Error()})
			return
		}
		code, err := p.client.PairPhone(r.Context(), phone, true, whatsmeow.PairClientChrome, "Chrome (Mac OS)")
		if err != nil {
			writeJSON(w, http.StatusBadGateway, PairResponse{State: "error", Message: fmt.Sprintf("WhatsApp refused the pairing code request: %v", err)})
			return
		}
		res := p.current()
		res.Code, res.QR = code, ""
		writeJSON(w, http.StatusOK, res)
	})
}

// useDataDir moves the bridge into dir (store/ and .env live there) when set,
// so an install outside the repo keeps its data in one private place.
func useDataDir(dir string) error {
	if dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create data directory: %v", err)
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return err
	}
	return os.Chdir(dir)
}

func bridgePort() int {
	if v, err := strconv.Atoi(strings.TrimSpace(os.Getenv("WHATSAPP_MCP_BRIDGE_PORT"))); err == nil && v > 0 && v < 65536 {
		return v
	}
	return 8080
}

// lockFile is kept for the life of the process: if it were garbage collected,
// closing it would silently release the lock.
var lockFile *os.File

// lockStore makes sure only one bridge uses a session at a time; two clients
// on one WhatsApp session knock each other offline. The lock is released when
// the process exits.
func lockStore() (bool, error) {
	f, err := os.OpenFile(storeDir+"/bridge.lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return false, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return false, nil
	}
	lockFile = f
	return true, nil
}
