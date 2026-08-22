package telephony

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/amcchord/ringring/internal/model"
)

type RoutingSource interface {
	RoutingDevices(context.Context) ([]model.RoutingDevice, error)
	RoutingServices(context.Context) ([]model.RoutingServices, error)
}

type SecretDecryptor interface {
	Decrypt(string, []byte) (string, error)
}

type Reloader interface {
	Reload(context.Context) error
}

type Reconciler struct {
	Source    RoutingSource
	Cipher    SecretDecryptor
	ConfigDir string
	Reloader  Reloader
}

func (r *Reconciler) Reconcile(ctx context.Context) error {
	if r == nil || r.ConfigDir == "" {
		return nil
	}
	devices, err := r.Source.RoutingDevices(ctx)
	if err != nil {
		return err
	}
	dialDevices, err := FromRoutingDevices(devices, r.Cipher.Decrypt)
	if err != nil {
		return err
	}
	services, err := r.Source.RoutingServices(ctx)
	if err != nil {
		return err
	}
	config, err := Render(dialDevices, services)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(r.ConfigDir, 0o750); err != nil {
		return fmt.Errorf("create Asterisk config directory: %w", err)
	}
	if err := atomicWrite(filepath.Join(r.ConfigDir, "pjsip.conf"), config.PJSIP); err != nil {
		return err
	}
	if err := atomicWrite(filepath.Join(r.ConfigDir, "extensions.conf"), config.Dialplan); err != nil {
		return err
	}
	if r.Reloader != nil {
		if err := r.Reloader.Reload(ctx); err != nil {
			return fmt.Errorf("reload Asterisk: %w", err)
		}
	}
	return nil
}

func atomicWrite(path string, content []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".ringring-*")
	if err != nil {
		return fmt.Errorf("create temporary config: %w", err)
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err := file.Chmod(0o640); err != nil {
		_ = file.Close()
		return fmt.Errorf("set config permissions: %w", err)
	}
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		return fmt.Errorf("write config: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync config: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close config: %w", err)
	}
	if err := os.Rename(temporary, path); err != nil {
		return fmt.Errorf("replace config: %w", err)
	}
	return nil
}

type AMI struct {
	Address  string
	Username string
	Secret   string
	Timeout  time.Duration
}

func (a AMI) Reload(ctx context.Context) error {
	if a.Address == "" || a.Secret == "" {
		return nil
	}
	timeout := a.Timeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	dialer := net.Dialer{Timeout: timeout}
	connection, err := dialer.DialContext(ctx, "tcp", a.Address)
	if err != nil {
		return fmt.Errorf("connect to AMI: %w", err)
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(timeout))

	reader := bufio.NewReader(connection)
	if _, err := reader.ReadString('\n'); err != nil {
		return fmt.Errorf("read AMI greeting: %w", err)
	}
	login := "Action: Login\r\nUsername: " + a.Username + "\r\nSecret: " + a.Secret + "\r\nEvents: off\r\n\r\n"
	if _, err := connection.Write([]byte(login)); err != nil {
		return fmt.Errorf("write AMI login: %w", err)
	}
	if err := expectResponse(reader, "Success"); err != nil {
		return fmt.Errorf("AMI login: %w", err)
	}
	if _, err := connection.Write([]byte("Action: Command\r\nCommand: core reload\r\n\r\n")); err != nil {
		return fmt.Errorf("write AMI reload: %w", err)
	}
	if err := expectResponse(reader, "Success", "Follows"); err != nil {
		return fmt.Errorf("AMI reload: %w", err)
	}
	_, _ = connection.Write([]byte("Action: Logoff\r\n\r\n"))
	return nil
}

func expectResponse(reader *bufio.Reader, allowed ...string) error {
	var response string
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return err
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			break
		}
		if strings.HasPrefix(strings.ToLower(trimmed), "response:") {
			response = strings.TrimSpace(strings.TrimPrefix(trimmed, "Response:"))
		}
	}
	if response == "" {
		return errors.New("response had no status")
	}
	for _, value := range allowed {
		if strings.EqualFold(response, value) {
			return nil
		}
	}
	return fmt.Errorf("response was %s", response)
}
