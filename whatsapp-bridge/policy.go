package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// writePolicy decides whether a write (send, group admin) is allowed.
// It is the bridge half of the write gate; the MCP server also hides write
// tools unless WHATSAPP_MCP_ALLOW_WRITE is set.
type writePolicy struct {
	allowWrite bool
	// allowlist holds normalised JIDs ("user@server"). Empty means no allowlist.
	allowlist map[string]bool
	// mediaDir, if set, is the only directory media may be sent from.
	mediaDir string
	// storeDir is never allowed as a media source (session keys, DBs, token).
	storeDir string
}

var errWriteDisabled = errors.New("writing is disabled: start the bridge with WHATSAPP_MCP_ALLOW_WRITE=true to send messages")

func loadWritePolicy() (*writePolicy, error) {
	p := &writePolicy{
		allowWrite: strings.EqualFold(strings.TrimSpace(os.Getenv("WHATSAPP_MCP_ALLOW_WRITE")), "true"),
		allowlist:  map[string]bool{},
	}
	for _, entry := range strings.Split(os.Getenv("WHATSAPP_MCP_SEND_ALLOWLIST"), ",") {
		if strings.TrimSpace(entry) == "" {
			continue
		}
		jid, err := normaliseRecipient(entry)
		if err != nil {
			return nil, fmt.Errorf("invalid WHATSAPP_MCP_SEND_ALLOWLIST entry %q: %v", entry, err)
		}
		p.allowlist[jid] = true
	}
	var err error
	if p.storeDir, err = realPath(storeDir); err != nil {
		return nil, err
	}
	if dir := strings.TrimSpace(os.Getenv("WHATSAPP_MCP_MEDIA_DIR")); dir != "" {
		if p.mediaDir, err = realPath(dir); err != nil {
			return nil, fmt.Errorf("invalid WHATSAPP_MCP_MEDIA_DIR: %v", err)
		}
	}
	return p, nil
}

// normaliseRecipient turns a phone number or JID into "user@server".
// Device suffixes (":12") are dropped so any device of a contact matches.
func normaliseRecipient(recipient string) (string, error) {
	r := strings.TrimSpace(recipient)
	if strings.Contains(r, "@") {
		parts := strings.SplitN(r, "@", 2)
		user := strings.SplitN(parts[0], ":", 2)[0]
		user = strings.SplitN(user, ".", 2)[0]
		if user == "" || parts[1] == "" {
			return "", fmt.Errorf("malformed JID")
		}
		return user + "@" + parts[1], nil
	}
	var digits strings.Builder
	for _, c := range r {
		switch {
		case c >= '0' && c <= '9':
			digits.WriteRune(c)
		case c == '+' || c == ' ' || c == '-' || c == '(' || c == ')':
		default:
			return "", fmt.Errorf("not a phone number or JID")
		}
	}
	if digits.Len() == 0 {
		return "", fmt.Errorf("empty phone number")
	}
	return digits.String() + "@s.whatsapp.net", nil
}

// checkWrite is called before any write action.
func (p *writePolicy) checkWrite(recipient string) error {
	if !p.allowWrite {
		return errWriteDisabled
	}
	if len(p.allowlist) == 0 {
		return nil
	}
	jid, err := normaliseRecipient(recipient)
	if err != nil {
		return fmt.Errorf("invalid recipient: %v", err)
	}
	if !p.allowlist[jid] {
		return fmt.Errorf("recipient %s is not on WHATSAPP_MCP_SEND_ALLOWLIST", jid)
	}
	return nil
}

// checkMediaPath stops the bridge from sending files out of its own store,
// and outside WHATSAPP_MCP_MEDIA_DIR when that is set.
func (p *writePolicy) checkMediaPath(path string) error {
	if path == "" {
		return nil
	}
	real, err := realPath(path)
	if err != nil {
		return fmt.Errorf("cannot read media file: %v", err)
	}
	if within(real, p.storeDir) {
		return fmt.Errorf("refusing to send a file from the bridge store directory")
	}
	if p.mediaDir != "" && !within(real, p.mediaDir) {
		return fmt.Errorf("media file must be inside WHATSAPP_MCP_MEDIA_DIR")
	}
	return nil
}

func realPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(abs)
}

func within(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
