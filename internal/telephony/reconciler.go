package telephony

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

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
