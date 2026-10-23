package config

import "testing"

func TestConfigLoad_ArchiveDefaults(t *testing.T) {
	t.Setenv("IMAGEKIT_PRIVATE_KEY", "")
	t.Setenv("IMAGEKIT_FOLDER", "")
	t.Setenv("ARCHIVE_MAX_MB", "")

	cfg := Load()
	if cfg.ArchiveMaxMB != 20 {
		t.Errorf("ArchiveMaxMB = %d, want 20", cfg.ArchiveMaxMB)
	}
	if cfg.ImageKitFolder != "/toolbox-pastes" {
		t.Errorf("ImageKitFolder = %q", cfg.ImageKitFolder)
	}
	if cfg.ImageKitPrivateKey != "" {
		t.Errorf("ImageKitPrivateKey = %q, want empty", cfg.ImageKitPrivateKey)
	}
}
