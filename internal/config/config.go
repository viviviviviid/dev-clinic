package config

import (
	"log"
	"os"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

type SupabaseConfig struct {
	URL     string `toml:"url"`
	AnonKey string `toml:"anon_key"`
}

type Config struct {
	AIProvider string         `toml:"ai_provider"`
	Codex      CodexConfig    `toml:"codex"`
	Gemini     GeminiConfig   `toml:"gemini"`
	Server     ServerConfig   `toml:"server"`
	Supabase   SupabaseConfig `toml:"supabase"`
	SiteURL    string         `toml:"site_url"`
	BaseDir    string
}

type CodexConfig struct {
	// Executable may be an absolute path or a command discoverable through PATH.
	Executable string `toml:"executable"`
	// Model is optional. When empty, codex exec uses the model associated with
	// the user's existing Codex CLI authentication and product defaults.
	Model string `toml:"model"`
}

type GeminiConfig struct {
	APIKey string `toml:"api_key"`
	Model  string `toml:"model"`
}

type ServerConfig struct {
	Port string `toml:"port"`
}

var Global = &Config{
	SiteURL:    "https://tutor.abcfe.net",
	AIProvider: "codex",
	Codex: CodexConfig{
		Executable: "codex",
	},
	Gemini: GeminiConfig{
		Model: "gemini-3.6-flash",
	},
	Server: ServerConfig{
		Port: "47291",
	},
}

func Load(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		log.Printf("config: %s not found, using defaults & env vars", path)
		applyEnv()
		return
	}
	if err := toml.Unmarshal(data, Global); err != nil {
		log.Fatalf("config: failed to parse %s: %v", path, err)
	}
	applyEnv()
	log.Printf("config: loaded %s (ai_provider=%s)", path, Global.AIProvider)
}

// applyEnv overrides config with environment variables
func applyEnv() {
	if v := strings.ToLower(strings.TrimSpace(os.Getenv("AI_PROVIDER"))); v != "" {
		Global.AIProvider = v
	}
	if v := strings.TrimSpace(os.Getenv("CODEX_MODEL")); v != "" {
		Global.Codex.Model = v
	}
	if v := strings.TrimSpace(os.Getenv("CODEX_BIN")); v != "" {
		Global.Codex.Executable = v
	}
	if v := os.Getenv("GEMINI_API_KEY"); v != "" {
		Global.Gemini.APIKey = v
	}
	if v := os.Getenv("GEMINI_MODEL"); v != "" {
		Global.Gemini.Model = v
	}
	if v := os.Getenv("PORT"); v != "" {
		Global.Server.Port = v
	}
	if v := os.Getenv("SUPABASE_URL"); v != "" {
		Global.Supabase.URL = v
	}
	if v := os.Getenv("SUPABASE_ANON_KEY"); v != "" {
		Global.Supabase.AnonKey = v
	}
	if v := strings.TrimSpace(os.Getenv("CLINIC_SITE_URL")); v != "" {
		Global.SiteURL = v
	}
	if v := os.Getenv("BASE_DIR"); v != "" {
		Global.BaseDir = v
	}
}
