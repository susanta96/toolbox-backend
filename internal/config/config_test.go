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

func TestConfigLoad_TrustProxyIPHeader(t *testing.T) {
	cases := []struct {
		env  string
		want bool
	}{
		{"", false},
		{"false", false},
		{"garbage", false},
		{"true", true},
		{"1", true},
	}
	for _, tc := range cases {
		t.Setenv("TRUST_PROXY_IP_HEADER", tc.env)
		if got := Load().TrustProxyIPHeader; got != tc.want {
			t.Errorf("TRUST_PROXY_IP_HEADER=%q: got %v, want %v", tc.env, got, tc.want)
		}
	}
}

func TestConfigLoad_ArchiveMaxMBBounds(t *testing.T) {
	cases := []struct {
		env  string
		want int64
	}{
		{"", 20},
		{"abc", 20},
		{"0", 20},
		{"-5", 20},
		{"1", 1},
		{"49", 49},
		{"50", 49},
		{"500", 49},
	}
	for _, tc := range cases {
		t.Setenv("ARCHIVE_MAX_MB", tc.env)
		if got := Load().ArchiveMaxMB; got != tc.want {
			t.Errorf("ARCHIVE_MAX_MB=%q: got %d, want %d", tc.env, got, tc.want)
		}
	}
}
