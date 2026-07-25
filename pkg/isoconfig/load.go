package isoconfig

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"go.uber.org/zap"
)

// Load finds the config-drive via loc (mounting it if necessary), reads
// pc-meta-data.json from it, and caches a copy at cachePath for future
// boots. If the config-drive isn't available, or reading/mounting it
// fails, it falls back to the most recent cached copy — which itself may
// not exist, in which case an empty (non-nil) ISOMetadata is returned, same
// contract Reader.Read() has always had.
func Load(ctx context.Context, loc Locator, mountPath, cachePath, label string, logger *zap.Logger) (*ISOMetadata, error) {
	dir, unmount, err := loc.Locate(ctx, mountPath, label)
	defer unmount()

	if err == nil && dir != "" {
		meta, readErr := NewReader(dir).Read()
		if readErr == nil && meta.Raw() != nil {
			if cacheErr := cacheCopy(dir, cachePath); cacheErr != nil {
				logger.Warn("could not cache pc-meta-data.json", zap.Error(cacheErr))
			}
			logger.Info("loaded pc-meta-data.json from config-drive", zap.String("dir", dir))
			return meta, nil
		}
		if readErr != nil {
			logger.Warn("could not read pc-meta-data.json from config-drive, falling back to cache", zap.Error(readErr))
		}
	} else if err != nil {
		logger.Warn("could not locate/mount config-drive, falling back to cache", zap.Error(err))
	}

	meta, err := NewReader(filepath.Dir(cachePath)).Read()
	if err != nil {
		return nil, fmt.Errorf("reading cached pc-meta-data.json: %w", err)
	}
	if meta.Raw() != nil {
		logger.Info("loaded pc-meta-data.json from local cache", zap.String("cache_path", cachePath))
	} else {
		logger.Debug("no config-drive and no cached pc-meta-data.json — using config-file identity")
	}
	return meta, nil
}

// cacheCopy copies srcDir/pc-meta-data.json to cachePath atomically (write
// to a temp file in the same directory, then rename) with 0600 permissions
// — the file carries a plaintext OS password and the NATS agent_api_key.
func cacheCopy(srcDir, cachePath string) error {
	data, err := os.ReadFile(filepath.Join(srcDir, "pc-meta-data.json"))
	if err != nil {
		return fmt.Errorf("reading source file: %w", err)
	}

	cacheDir := filepath.Dir(cachePath)
	if err := os.MkdirAll(cacheDir, 0700); err != nil {
		return fmt.Errorf("creating cache dir: %w", err)
	}

	tmp := cachePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return fmt.Errorf("writing temp cache file: %w", err)
	}
	if err := os.Rename(tmp, cachePath); err != nil {
		return fmt.Errorf("renaming temp cache file: %w", err)
	}
	return nil
}
