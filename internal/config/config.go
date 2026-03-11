package config

import (
	"log"
	"os"

	"github.com/pelletier/go-toml/v2"
)

type SupabaseConfig struct {
	URL            string `toml:"url"`
	AnonKey        string `toml:"anon_key"`
	ServiceRoleKey string `toml:"service_role_key"`
	JWTSecret      string `toml:"jwt_secret"`
}

type Config struct {
	Gemini   GeminiConfig   `toml:"gemini"`
	Server   ServerConfig   `toml:"server"`
	Supabase SupabaseConfig `toml:"supabase"`
}

type GeminiConfig struct {
	APIKey string `toml:"api_key"`
	Model  string `toml:"model"`
}

type ServerConfig struct {
	Port string `toml:"port"`
}

var Global = &Config{
	Gemini: GeminiConfig{
		Model: "gemini-2.5-flash-lite-preview-06-17",
	},
	Server: ServerConfig{
		Port: "8080",
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
	log.Printf("config: loaded %s (model=%s)", path, Global.Gemini.Model)
}

// applyEnv overrides config with environment variables
func applyEnv() {
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
	if v := os.Getenv("SUPABASE_SERVICE_ROLE_KEY"); v != "" {
		Global.Supabase.ServiceRoleKey = v
	}
	if v := os.Getenv("SUPABASE_JWT_SECRET"); v != "" {
		Global.Supabase.JWTSecret = v
	}
}
