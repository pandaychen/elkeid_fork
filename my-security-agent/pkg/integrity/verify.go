package integrity

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

var ErrHashMismatch = errors.New("hash mismatch")

// VerifyFile checks that the SHA-256 of path matches expectedHex.
func VerifyFile(path, expectedHex string) error {
	if expectedHex == "" {
		return errors.New("expected hash is empty")
	}
	expected, err := hex.DecodeString(expectedHex)
	if err != nil {
		return fmt.Errorf("invalid hex: %w", err)
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	if !bytes.Equal(h.Sum(nil), expected) {
		return ErrHashMismatch
	}
	return nil
}

// ComputeHash returns the hex-encoded SHA-256 of a file.
func ComputeHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// DownloadConfig configures a verified download.
type DownloadConfig struct {
	URLs         []string      // fallback URL list
	ExpectedHash string        // hex-encoded SHA-256
	MaxSize      int64         // max download bytes (default 512 MB)
	Timeout      time.Duration // HTTP timeout (default 10 min)
}

// DownloadVerified downloads dst, streaming SHA-256 verification.
// Returns nil immediately if the local file already matches.
func DownloadVerified(ctx context.Context, dst string, cfg DownloadConfig) error {
	if err := VerifyFile(dst, cfg.ExpectedHash); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0701); err != nil {
		return err
	}
	maxSize := cfg.MaxSize
	if maxSize == 0 {
		maxSize = 512 * 1024 * 1024
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 10 * time.Minute
	}
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: (&net.Dialer{Timeout: 15 * time.Second}).DialContext,
		},
		Timeout: timeout,
	}
	var lastErr error
	for _, url := range cfg.URLs {
		if err := downloadOne(ctx, client, url, dst, cfg.ExpectedHash, maxSize); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	return fmt.Errorf("all download URLs failed: %w", lastErr)
}

func downloadOne(ctx context.Context, client *http.Client, url, dst, expectHash string, maxSize int64) error {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %s", resp.Status)
	}
	body := http.MaxBytesReader(nil, resp.Body, maxSize)
	h := sha256.New()
	r := io.TeeReader(body, h)

	f, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0700)
	if err != nil {
		return err
	}
	_, cpErr := io.Copy(f, r)
	f.Close()
	if cpErr != nil {
		os.Remove(dst)
		return cpErr
	}
	actual := hex.EncodeToString(h.Sum(nil))
	if actual != expectHash {
		os.Remove(dst)
		return fmt.Errorf("%w: got %s want %s", ErrHashMismatch, actual, expectHash)
	}
	return nil
}
