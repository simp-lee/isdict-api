package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var configEnvKeys = []string{
	"DB_HOST",
	"DB_PORT",
	"DB_USER",
	"DB_PASSWORD",
	"DB_NAME",
	"DB_SSLMODE",
	"PGHOST",
	"PGPORT",
	"PGUSER",
	"PGPASSWORD",
	"PGDATABASE",
	"PGSSLMODE",
	"PORT",
	"GIN_MODE",
	"LOG_LEVEL",
	"LOG_OUTPUT",
	"LOG_FILE_PATH",
	"LOG_MAX_SIZE_MB",
	"LOG_MAX_BACKUPS",
	"LOG_MAX_AGE_DAYS",
	"DB_MAX_IDLE_CONNS",
	"DB_MAX_OPEN_CONNS",
	"API_BATCH_MAX_SIZE",
	"API_SEARCH_MAX_LIMIT",
	"API_SUGGEST_MAX_LIMIT",
	"ENABLE_RATE_LIMIT",
	"RATE_LIMIT_RPS",
	"RATE_LIMIT_BURST",
	"RATE_LIMIT_PER_HOUR",
	"RATE_LIMIT_PER_DAY",
	"ENABLE_CORS",
	"CORS_ALLOW_ORIGINS",
	"ENABLE_TIMEOUT",
	"TIMEOUT_SECONDS",
	"ENABLE_REQUEST_ID",
	"ENABLE_CACHE",
	"CACHE_MAX_SIZE",
	"CACHE_EXPIRATION_MINS",
	explicitEnvFileVar,
}

func clearConfigEnv(t *testing.T) {
	t.Helper()

	for _, key := range configEnvKeys {
		key := key
		value, exists := os.LookupEnv(key)
		if exists {
			t.Cleanup(func() {
				if err := os.Setenv(key, value); err != nil {
					t.Fatalf("Setenv(%q) restore error = %v", key, err)
				}
			})
		} else {
			t.Cleanup(func() {
				if err := os.Unsetenv(key); err != nil {
					t.Fatalf("Unsetenv(%q) restore error = %v", key, err)
				}
			})
		}
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("Unsetenv(%q) error = %v", key, err)
		}
	}
}

func writeEnvFile(t *testing.T, root, relativePath, content string) string {
	t.Helper()

	path := filepath.Join(root, relativePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}

	return path
}

func validConfigForValidationTest() Config {
	return Config{
		DBHost:             "localhost",
		DBPort:             "5432",
		DBUser:             "user",
		DBPassword:         "pass",
		DBName:             "db",
		Port:               "8080",
		DBMaxIdleConns:     10,
		DBMaxOpenConns:     100,
		GinMode:            "debug",
		APIBatchMaxSize:    100,
		APISearchMaxLimit:  100,
		APISuggestMaxLimit: 50,
	}
}

func TestConfig_Validate(t *testing.T) {
	validConfig := validConfigForValidationTest()

	tests := []struct {
		name    string
		config  Config
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid configuration",
			config:  validConfig,
			wantErr: false,
		},
		{
			name: "empty DB_HOST",
			config: Config{
				DBHost:             "",
				DBPort:             "5432",
				DBUser:             "user",
				DBPassword:         "pass",
				DBName:             "db",
				Port:               "8080",
				DBMaxIdleConns:     10,
				DBMaxOpenConns:     100,
				GinMode:            "debug",
				APIBatchMaxSize:    100,
				APISearchMaxLimit:  100,
				APISuggestMaxLimit: 50,
			},
			wantErr: true,
			errMsg:  "DB_HOST cannot be empty",
		},
		{
			name: "empty DB_NAME",
			config: Config{
				DBHost:             "localhost",
				DBPort:             "5432",
				DBUser:             "user",
				DBPassword:         "pass",
				DBName:             "",
				Port:               "8080",
				DBMaxIdleConns:     10,
				DBMaxOpenConns:     100,
				GinMode:            "debug",
				APIBatchMaxSize:    100,
				APISearchMaxLimit:  100,
				APISuggestMaxLimit: 50,
			},
			wantErr: true,
			errMsg:  "DB_NAME cannot be empty",
		},
		{
			name: "empty Port",
			config: Config{
				DBHost:             "localhost",
				DBPort:             "5432",
				DBUser:             "user",
				DBPassword:         "pass",
				DBName:             "db",
				Port:               "",
				DBMaxIdleConns:     10,
				DBMaxOpenConns:     100,
				GinMode:            "debug",
				APIBatchMaxSize:    100,
				APISearchMaxLimit:  100,
				APISuggestMaxLimit: 50,
			},
			wantErr: true,
			errMsg:  "PORT cannot be empty",
		},
		{
			name: "negative DBMaxIdleConns",
			config: Config{
				DBHost:             "localhost",
				DBPort:             "5432",
				DBUser:             "user",
				DBPassword:         "pass",
				DBName:             "db",
				Port:               "8080",
				DBMaxIdleConns:     -1,
				DBMaxOpenConns:     100,
				GinMode:            "debug",
				APIBatchMaxSize:    100,
				APISearchMaxLimit:  100,
				APISuggestMaxLimit: 50,
			},
			wantErr: true,
			errMsg:  "DB_MAX_IDLE_CONNS must be non-negative",
		},
		{
			name: "DBMaxOpenConns less than 1",
			config: Config{
				DBHost:             "localhost",
				DBPort:             "5432",
				DBUser:             "user",
				DBPassword:         "pass",
				DBName:             "db",
				Port:               "8080",
				DBMaxIdleConns:     10,
				DBMaxOpenConns:     0,
				GinMode:            "debug",
				APIBatchMaxSize:    100,
				APISearchMaxLimit:  100,
				APISuggestMaxLimit: 50,
			},
			wantErr: true,
			errMsg:  "DB_MAX_OPEN_CONNS must be at least 1",
		},
		{
			name: "DBMaxIdleConns exceeds DBMaxOpenConns",
			config: Config{
				DBHost:             "localhost",
				DBPort:             "5432",
				DBUser:             "user",
				DBPassword:         "pass",
				DBName:             "db",
				Port:               "8080",
				DBMaxIdleConns:     200,
				DBMaxOpenConns:     100,
				GinMode:            "debug",
				APIBatchMaxSize:    100,
				APISearchMaxLimit:  100,
				APISuggestMaxLimit: 50,
			},
			wantErr: true,
			errMsg:  "DB_MAX_IDLE_CONNS cannot exceed DB_MAX_OPEN_CONNS",
		},
		{
			name: "APIBatchMaxSize less than 1",
			config: Config{
				DBHost:             "localhost",
				DBPort:             "5432",
				DBUser:             "user",
				DBPassword:         "pass",
				DBName:             "db",
				Port:               "8080",
				DBMaxIdleConns:     10,
				DBMaxOpenConns:     100,
				GinMode:            "debug",
				APIBatchMaxSize:    0,
				APISearchMaxLimit:  100,
				APISuggestMaxLimit: 50,
			},
			wantErr: true,
			errMsg:  "API_BATCH_MAX_SIZE must be at least 1",
		},
		{
			name: "APISearchMaxLimit less than 1",
			config: Config{
				DBHost:             "localhost",
				DBPort:             "5432",
				DBUser:             "user",
				DBPassword:         "pass",
				DBName:             "db",
				Port:               "8080",
				DBMaxIdleConns:     10,
				DBMaxOpenConns:     100,
				GinMode:            "debug",
				APIBatchMaxSize:    100,
				APISearchMaxLimit:  0,
				APISuggestMaxLimit: 50,
			},
			wantErr: true,
			errMsg:  "API_SEARCH_MAX_LIMIT must be at least 1",
		},
		{
			name: "APISuggestMaxLimit less than 1",
			config: Config{
				DBHost:             "localhost",
				DBPort:             "5432",
				DBUser:             "user",
				DBPassword:         "pass",
				DBName:             "db",
				Port:               "8080",
				DBMaxIdleConns:     10,
				DBMaxOpenConns:     100,
				GinMode:            "debug",
				APIBatchMaxSize:    100,
				APISearchMaxLimit:  100,
				APISuggestMaxLimit: 0,
			},
			wantErr: true,
			errMsg:  "API_SUGGEST_MAX_LIMIT must be at least 1",
		},
		{
			name: "invalid gin mode",
			config: Config{
				DBHost:             "localhost",
				DBPort:             "5432",
				DBUser:             "user",
				DBPassword:         "pass",
				DBName:             "db",
				Port:               "8080",
				DBMaxIdleConns:     10,
				DBMaxOpenConns:     100,
				GinMode:            "prod",
				APIBatchMaxSize:    100,
				APISearchMaxLimit:  100,
				APISuggestMaxLimit: 50,
			},
			wantErr: true,
			errMsg:  "GIN_MODE must be one of: debug, release, test",
		},
		{
			name: "valid file log configuration",
			config: Config{
				DBHost:             "localhost",
				DBPort:             "5432",
				DBUser:             "user",
				DBPassword:         "pass",
				DBName:             "db",
				Port:               "8080",
				DBMaxIdleConns:     10,
				DBMaxOpenConns:     100,
				GinMode:            "release",
				APIBatchMaxSize:    100,
				APISearchMaxLimit:  100,
				APISuggestMaxLimit: 50,
				LogLevel:           "info",
				LogOutput:          "file",
				LogFilePath:        "/tmp/isdict-api.log",
				LogMaxSizeMB:       100,
				LogMaxBackups:      7,
				LogMaxAgeDays:      30,
			},
			wantErr: false,
		},
		{
			name: "invalid log level",
			config: Config{
				DBHost:             "localhost",
				DBPort:             "5432",
				DBUser:             "user",
				DBPassword:         "pass",
				DBName:             "db",
				Port:               "8080",
				DBMaxIdleConns:     10,
				DBMaxOpenConns:     100,
				GinMode:            "debug",
				APIBatchMaxSize:    100,
				APISearchMaxLimit:  100,
				APISuggestMaxLimit: 50,
				LogLevel:           "verbose",
			},
			wantErr: true,
			errMsg:  "LOG_LEVEL must be one of: debug, info, warn, error",
		},
		{
			name: "invalid log output",
			config: Config{
				DBHost:             "localhost",
				DBPort:             "5432",
				DBUser:             "user",
				DBPassword:         "pass",
				DBName:             "db",
				Port:               "8080",
				DBMaxIdleConns:     10,
				DBMaxOpenConns:     100,
				GinMode:            "debug",
				APIBatchMaxSize:    100,
				APISearchMaxLimit:  100,
				APISuggestMaxLimit: 50,
				LogOutput:          "stderr",
			},
			wantErr: true,
			errMsg:  "LOG_OUTPUT must be one of: stdout, file",
		},
		{
			name: "file log output requires path",
			config: Config{
				DBHost:             "localhost",
				DBPort:             "5432",
				DBUser:             "user",
				DBPassword:         "pass",
				DBName:             "db",
				Port:               "8080",
				DBMaxIdleConns:     10,
				DBMaxOpenConns:     100,
				GinMode:            "release",
				APIBatchMaxSize:    100,
				APISearchMaxLimit:  100,
				APISuggestMaxLimit: 50,
				LogOutput:          "file",
				LogFilePath:        "",
			},
			wantErr: true,
			errMsg:  "LOG_FILE_PATH cannot be empty when LOG_OUTPUT=file",
		},
		{
			name: "negative log max size",
			config: Config{
				DBHost:             "localhost",
				DBPort:             "5432",
				DBUser:             "user",
				DBPassword:         "pass",
				DBName:             "db",
				Port:               "8080",
				DBMaxIdleConns:     10,
				DBMaxOpenConns:     100,
				GinMode:            "release",
				APIBatchMaxSize:    100,
				APISearchMaxLimit:  100,
				APISuggestMaxLimit: 50,
				LogOutput:          "file",
				LogFilePath:        "/tmp/isdict-api.log",
				LogMaxSizeMB:       -1,
			},
			wantErr: true,
			errMsg:  "LOG_MAX_SIZE_MB must be non-negative",
		},
		{
			name: "negative log max backups",
			config: Config{
				DBHost:             "localhost",
				DBPort:             "5432",
				DBUser:             "user",
				DBPassword:         "pass",
				DBName:             "db",
				Port:               "8080",
				DBMaxIdleConns:     10,
				DBMaxOpenConns:     100,
				GinMode:            "release",
				APIBatchMaxSize:    100,
				APISearchMaxLimit:  100,
				APISuggestMaxLimit: 50,
				LogOutput:          "file",
				LogFilePath:        "/tmp/isdict-api.log",
				LogMaxBackups:      -1,
			},
			wantErr: true,
			errMsg:  "LOG_MAX_BACKUPS must be non-negative",
		},
		{
			name: "negative log max age",
			config: Config{
				DBHost:             "localhost",
				DBPort:             "5432",
				DBUser:             "user",
				DBPassword:         "pass",
				DBName:             "db",
				Port:               "8080",
				DBMaxIdleConns:     10,
				DBMaxOpenConns:     100,
				GinMode:            "release",
				APIBatchMaxSize:    100,
				APISearchMaxLimit:  100,
				APISuggestMaxLimit: 50,
				LogOutput:          "file",
				LogFilePath:        "/tmp/isdict-api.log",
				LogMaxAgeDays:      -1,
			},
			wantErr: true,
			errMsg:  "LOG_MAX_AGE_DAYS must be non-negative",
		},
		{
			name: "timeout enabled requires positive seconds",
			config: func() Config {
				cfg := validConfig
				cfg.EnableTimeout = true
				cfg.TimeoutSeconds = 0
				return cfg
			}(),
			wantErr: true,
			errMsg:  "TIMEOUT_SECONDS must be at least 1 when ENABLE_TIMEOUT=true",
		},
		{
			name: "rate limit rps must be non-negative",
			config: func() Config {
				cfg := validConfig
				cfg.EnableRateLimit = true
				cfg.RateLimitRPS = -1
				return cfg
			}(),
			wantErr: true,
			errMsg:  "RATE_LIMIT_RPS must be non-negative when ENABLE_RATE_LIMIT=true",
		},
		{
			name: "rate limit burst requires positive value when rps enabled",
			config: func() Config {
				cfg := validConfig
				cfg.EnableRateLimit = true
				cfg.RateLimitRPS = 10
				cfg.RateLimitBurst = 0
				return cfg
			}(),
			wantErr: true,
			errMsg:  "RATE_LIMIT_BURST must be at least 1 when RATE_LIMIT_RPS > 0",
		},
		{
			name: "rate limit per hour must be non-negative",
			config: func() Config {
				cfg := validConfig
				cfg.EnableRateLimit = true
				cfg.RateLimitPerHour = -1
				return cfg
			}(),
			wantErr: true,
			errMsg:  "RATE_LIMIT_PER_HOUR must be non-negative when ENABLE_RATE_LIMIT=true",
		},
		{
			name: "cache max size must be non-negative",
			config: func() Config {
				cfg := validConfig
				cfg.EnableCache = true
				cfg.CacheMaxSize = -1
				return cfg
			}(),
			wantErr: true,
			errMsg:  "CACHE_MAX_SIZE must be non-negative when ENABLE_CACHE=true",
		},
		{
			name: "cache expiration must be non-negative",
			config: func() Config {
				cfg := validConfig
				cfg.EnableCache = true
				cfg.CacheExpirationMins = -1
				return cfg
			}(),
			wantErr: true,
			errMsg:  "CACHE_EXPIRATION_MINS must be non-negative when ENABLE_CACHE=true",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Config.Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && err.Error() != tt.errMsg {
				t.Errorf("Config.Validate() error message = %v, want %v", err.Error(), tt.errMsg)
			}
		})
	}
}

func TestConfig_GetDSN(t *testing.T) {
	cfg := Config{
		DBHost:     "testhost",
		DBPort:     "5433",
		DBUser:     "testuser",
		DBPassword: "testpass",
		DBName:     "testdb",
		DBSSLMode:  "disable",
	}

	expected := "host=testhost port=5433 user=testuser password=testpass dbname=testdb sslmode=disable TimeZone=Asia/Shanghai"
	got := cfg.GetDSN()

	if got != expected {
		t.Errorf("Config.GetDSN() = %v, want %v", got, expected)
	}
}

func TestGetEnv(t *testing.T) {
	tests := []struct {
		name         string
		key          string
		defaultValue string
		envValue     string
		want         string
	}{
		{
			name:         "environment variable set",
			key:          "TEST_ENV_VAR",
			defaultValue: "default",
			envValue:     "custom",
			want:         "custom",
		},
		{
			name:         "environment variable not set",
			key:          "TEST_ENV_VAR_NOT_SET",
			defaultValue: "default",
			envValue:     "",
			want:         "default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue != "" {
				t.Setenv(tt.key, tt.envValue)
			}

			got := getEnv(tt.key, tt.defaultValue)
			if got != tt.want {
				t.Errorf("getEnv() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetEnvAsInt(t *testing.T) {
	tests := []struct {
		name         string
		key          string
		defaultValue int
		envValue     string
		want         int
	}{
		{
			name:         "valid integer",
			key:          "TEST_INT_VAR",
			defaultValue: 10,
			envValue:     "42",
			want:         42,
		},
		{
			name:         "empty string uses default",
			key:          "TEST_INT_VAR_EMPTY",
			defaultValue: 10,
			envValue:     "",
			want:         10,
		},
		{
			name:         "invalid integer uses default",
			key:          "TEST_INT_VAR_INVALID",
			defaultValue: 10,
			envValue:     "invalid",
			want:         10,
		},
		{
			name:         "negative integer",
			key:          "TEST_INT_VAR_NEGATIVE",
			defaultValue: 10,
			envValue:     "-5",
			want:         -5,
		},
		{
			name:         "zero value",
			key:          "TEST_INT_VAR_ZERO",
			defaultValue: 10,
			envValue:     "0",
			want:         0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue != "" {
				t.Setenv(tt.key, tt.envValue)
			}

			got := getEnvAsInt(tt.key, tt.defaultValue)
			if got != tt.want {
				t.Errorf("getEnvAsInt() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLoad_DefaultValues(t *testing.T) {
	tempDir := t.TempDir()
	t.Chdir(tempDir)
	clearConfigEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if err := cfg.Validate(); err != nil {
		t.Errorf("Loaded config should be valid, got error: %v", err)
	}
	assertDefaultConfigStrings(t, cfg)
	assertDefaultConfigInts(t, cfg)
}

func TestLoad_EnvFilePrecedence(t *testing.T) {
	t.Run("explicit env file overrides fallback files", runExplicitEnvFileOverridesFallbackFiles)
	t.Run("process env overrides explicit env file", runProcessEnvOverridesExplicitEnvFile)
	t.Run("fallback files are checked in order", func(t *testing.T) {
		tempDir := t.TempDir()
		projectDir := filepath.Join(tempDir, "project")
		if err := os.MkdirAll(projectDir, 0o755); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v", projectDir, err)
		}
		t.Chdir(projectDir)
		clearConfigEnv(t)

		writeEnvFile(t, projectDir, "api/.env", "DB_HOST=api-dotenv\n")
		writeEnvFile(t, tempDir, ".env", "DB_HOST=parent-dotenv\n")
		writeEnvFile(t, projectDir, ".env", "DB_HOST=current-dotenv\n")

		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}

		if cfg.DBHost != "current-dotenv" {
			t.Fatalf("Load() DBHost = %q, want %q", cfg.DBHost, "current-dotenv")
		}
	})

	t.Run("missing explicit env file returns an error", func(t *testing.T) {
		tempDir := t.TempDir()
		t.Chdir(tempDir)
		clearConfigEnv(t)
		t.Setenv(explicitEnvFileVar, filepath.Join(tempDir, "missing.env"))

		if _, err := Load(); err == nil {
			t.Fatal("Load() error = nil, want error")
		}
	})

	t.Run("successful explicit env file skips malformed fallback files", func(t *testing.T) {
		tempDir := t.TempDir()
		t.Chdir(tempDir)
		clearConfigEnv(t)

		explicitPath := writeEnvFile(t, tempDir, "configs/api.env", "DB_HOST=explicit-host\nLOG_OUTPUT=file\nLOG_FILE_PATH=/tmp/isdict-api.log\n")
		writeEnvFile(t, tempDir, ".env", "BROKEN='unterminated\n")
		t.Setenv(explicitEnvFileVar, explicitPath)

		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}

		if cfg.DBHost != "explicit-host" {
			t.Fatalf("Load() DBHost = %q, want %q", cfg.DBHost, "explicit-host")
		}
		if cfg.LogOutput != "file" {
			t.Fatalf("Load() LogOutput = %q, want %q", cfg.LogOutput, "file")
		}
		if cfg.LogFilePath != "/tmp/isdict-api.log" {
			t.Fatalf("Load() LogFilePath = %q, want %q", cfg.LogFilePath, "/tmp/isdict-api.log")
		}
	})

	t.Run("malformed fallback env file returns an error", func(t *testing.T) {
		tempDir := t.TempDir()
		t.Chdir(tempDir)
		clearConfigEnv(t)

		writeEnvFile(t, tempDir, ".env", "BROKEN='unterminated\n")

		_, err := Load()
		if err == nil {
			t.Fatal("Load() error = nil, want error")
		}
		if got := err.Error(); !strings.HasPrefix(got, "load fallback env file") {
			t.Fatalf("Load() error = %q, want fallback env load failure", got)
		}
	})
}

func assertDefaultConfigStrings(t *testing.T, cfg *Config) {
	t.Helper()
	checks := []struct{ name, got, want string }{
		{name: "DBHost", got: cfg.DBHost, want: "localhost"},
		{name: "DBPort", got: cfg.DBPort, want: "5432"},
		{name: "DBUser", got: cfg.DBUser, want: "postgres"},
		{name: "DBPassword", got: cfg.DBPassword, want: "postgres"},
		{name: "DBName", got: cfg.DBName, want: "isdict"},
		{name: "DBSSLMode", got: cfg.DBSSLMode, want: "prefer"},
		{name: "Port", got: cfg.Port, want: "8080"},
		{name: "GinMode", got: cfg.GinMode, want: "debug"},
		{name: "LogLevel", got: cfg.LogLevel, want: "debug"},
		{name: "LogOutput", got: cfg.LogOutput, want: "stdout"},
	}
	for _, check := range checks {
		if check.got != check.want {
			t.Errorf("Load() %s = %q, want %q", check.name, check.got, check.want)
		}
	}
}

func assertDefaultConfigInts(t *testing.T, cfg *Config) {
	t.Helper()
	checks := []struct {
		name      string
		got, want int
	}{
		{name: "LogMaxSizeMB", got: cfg.LogMaxSizeMB, want: 100},
		{name: "LogMaxBackups", got: cfg.LogMaxBackups, want: 7},
		{name: "LogMaxAgeDays", got: cfg.LogMaxAgeDays, want: 30},
	}
	for _, check := range checks {
		if check.got != check.want {
			t.Errorf("Load() %s = %d, want %d", check.name, check.got, check.want)
		}
	}
}

func runExplicitEnvFileOverridesFallbackFiles(t *testing.T) {
	tempDir := t.TempDir()
	t.Chdir(tempDir)
	clearConfigEnv(t)

	explicitPath := writeEnvFile(t, tempDir, "configs/api.env", "DB_HOST=explicit-host\nLOG_OUTPUT=file\nLOG_FILE_PATH=/tmp/isdict-api.log\n")
	writeEnvFile(t, tempDir, ".env", "DB_HOST=dotenv-host\nLOG_OUTPUT=stdout\n")
	t.Setenv(explicitEnvFileVar, explicitPath)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.DBHost != "explicit-host" || cfg.LogOutput != "file" || cfg.LogFilePath != "/tmp/isdict-api.log" {
		t.Fatalf("unexpected explicit env file config: %+v", cfg)
	}
}

func runProcessEnvOverridesExplicitEnvFile(t *testing.T) {
	tempDir := t.TempDir()
	t.Chdir(tempDir)
	clearConfigEnv(t)

	explicitPath := writeEnvFile(t, tempDir, "configs/api.env", "DB_HOST=explicit-host\n")
	t.Setenv(explicitEnvFileVar, explicitPath)
	t.Setenv("DB_HOST", "process-host")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.DBHost != "process-host" {
		t.Fatalf("Load() DBHost = %q, want %q", cfg.DBHost, "process-host")
	}
}

func TestLoad_PostgresEnvAliases(t *testing.T) {
	t.Run("postgres aliases populate database settings", func(t *testing.T) {
		tempDir := t.TempDir()
		t.Chdir(tempDir)
		clearConfigEnv(t)

		t.Setenv("PGHOST", "pg-host")
		t.Setenv("PGPORT", "6543")
		t.Setenv("PGUSER", "pg-user")
		t.Setenv("PGPASSWORD", "pg-pass")
		t.Setenv("PGDATABASE", "pg-db")
		t.Setenv("PGSSLMODE", "require")

		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}

		if cfg.DBHost != "pg-host" {
			t.Fatalf("Load() DBHost = %q, want %q", cfg.DBHost, "pg-host")
		}
		if cfg.DBPort != "6543" {
			t.Fatalf("Load() DBPort = %q, want %q", cfg.DBPort, "6543")
		}
		if cfg.DBUser != "pg-user" {
			t.Fatalf("Load() DBUser = %q, want %q", cfg.DBUser, "pg-user")
		}
		if cfg.DBPassword != "pg-pass" {
			t.Fatalf("Load() DBPassword = %q, want %q", cfg.DBPassword, "pg-pass")
		}
		if cfg.DBName != "pg-db" {
			t.Fatalf("Load() DBName = %q, want %q", cfg.DBName, "pg-db")
		}
		if cfg.DBSSLMode != "require" {
			t.Fatalf("Load() DBSSLMode = %q, want %q", cfg.DBSSLMode, "require")
		}
	})

	t.Run("db variables override postgres aliases", func(t *testing.T) {
		tempDir := t.TempDir()
		t.Chdir(tempDir)
		clearConfigEnv(t)

		t.Setenv("PGHOST", "pg-host")
		t.Setenv("PGPASSWORD", "pg-pass")
		t.Setenv("PGSSLMODE", "require")
		t.Setenv("DB_HOST", "db-host")
		t.Setenv("DB_PASSWORD", "db-pass")
		t.Setenv("DB_SSLMODE", "disable")

		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}

		if cfg.DBHost != "db-host" {
			t.Fatalf("Load() DBHost = %q, want %q", cfg.DBHost, "db-host")
		}
		if cfg.DBPassword != "db-pass" {
			t.Fatalf("Load() DBPassword = %q, want %q", cfg.DBPassword, "db-pass")
		}
		if cfg.DBSSLMode != "disable" {
			t.Fatalf("Load() DBSSLMode = %q, want %q", cfg.DBSSLMode, "disable")
		}
	})
}

func TestLoad_InvalidGinModeReturnsValidationError(t *testing.T) {
	tempDir := t.TempDir()
	t.Chdir(tempDir)
	clearConfigEnv(t)
	t.Setenv("GIN_MODE", "prod")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want error")
	}

	const want = "invalid configuration: GIN_MODE must be one of: debug, release, test"
	if err.Error() != want {
		t.Fatalf("Load() error = %q, want %q", err.Error(), want)
	}
}

func TestLoad_LogDefaults(t *testing.T) {
	tests := []struct {
		name          string
		ginMode       string
		wantLogLevel  string
		wantLogOutput string
	}{
		{
			name:          "debug mode uses stdout defaults",
			ginMode:       "debug",
			wantLogLevel:  "debug",
			wantLogOutput: "stdout",
		},
		{
			name:          "test mode uses stdout defaults",
			ginMode:       "test",
			wantLogLevel:  "debug",
			wantLogOutput: "stdout",
		},
		{
			name:          "release mode uses info level by default",
			ginMode:       "release",
			wantLogLevel:  "info",
			wantLogOutput: "stdout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()
			t.Chdir(tempDir)
			clearConfigEnv(t)

			t.Setenv("GIN_MODE", tt.ginMode)

			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}

			if cfg.LogLevel != tt.wantLogLevel {
				t.Fatalf("Load() LogLevel = %q, want %q", cfg.LogLevel, tt.wantLogLevel)
			}
			if cfg.LogOutput != tt.wantLogOutput {
				t.Fatalf("Load() LogOutput = %q, want %q", cfg.LogOutput, tt.wantLogOutput)
			}
			if cfg.LogMaxSizeMB != 100 {
				t.Fatalf("Load() LogMaxSizeMB = %d, want %d", cfg.LogMaxSizeMB, 100)
			}
			if cfg.LogMaxBackups != 7 {
				t.Fatalf("Load() LogMaxBackups = %d, want %d", cfg.LogMaxBackups, 7)
			}
			if cfg.LogMaxAgeDays != 30 {
				t.Fatalf("Load() LogMaxAgeDays = %d, want %d", cfg.LogMaxAgeDays, 30)
			}
		})
	}
}

func TestLoad_CanonicalizesValidatedLogConfig(t *testing.T) {
	tempDir := t.TempDir()
	t.Chdir(tempDir)
	clearConfigEnv(t)
	wantLogFilePath := filepath.Join(tempDir, "isdict-api.log")

	t.Setenv("LOG_LEVEL", " INFO ")
	t.Setenv("LOG_OUTPUT", " FILE ")
	t.Setenv("LOG_FILE_PATH", " \n\t"+wantLogFilePath+" \t\n")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.LogLevel != "info" {
		t.Fatalf("Load() LogLevel = %q, want %q", cfg.LogLevel, "info")
	}
	if cfg.LogOutput != "file" {
		t.Fatalf("Load() LogOutput = %q, want %q", cfg.LogOutput, "file")
	}
	if cfg.LogFilePath != wantLogFilePath {
		t.Fatalf("Load() LogFilePath = %q, want %q", cfg.LogFilePath, wantLogFilePath)
	}
}

// AC-R023: .gitignore 必须覆盖 runtime 会加载的本地 env 文件
func TestGitignoreCoversRuntimeEnvFiles(t *testing.T) {
	// REG-023

	// Env file paths that loadEnvFile may read at runtime.
	runtimeEnvPaths := []string{
		".env",
		"api/.env",
		"configs/api.env",
	}

	gitignorePath := filepath.Join(findRepoRoot(t), ".gitignore")
	patterns := readGitignorePatterns(t, gitignorePath)

	for _, envPath := range runtimeEnvPaths {
		if !gitignoreCovers(patterns, envPath) {
			t.Errorf(".gitignore does not cover runtime env file %q", envPath)
		}
	}
}

// findRepoRoot walks up from the current working directory until it finds .gitignore.
func findRepoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, ".gitignore")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repository root containing .gitignore")
		}
		dir = parent
	}
}

// readGitignorePatterns reads non-blank, non-comment lines from a .gitignore file.
func readGitignorePatterns(t *testing.T, path string) []string {
	t.Helper()

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open(%q) error = %v", path, err)
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			t.Fatalf("Close(%q) error = %v", path, closeErr)
		}
	}()

	var patterns []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		patterns = append(patterns, line)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanning %q: %v", path, err)
	}
	return patterns
}

// gitignoreCovers checks whether any pattern in the .gitignore matches the given path.
// It supports exact matches and simple prefix-directory patterns (e.g. "bin/").
func gitignoreCovers(patterns []string, target string) bool {
	normalized := filepath.ToSlash(target)
	for _, pat := range patterns {
		pat = filepath.ToSlash(pat)
		if pat == normalized {
			return true
		}
		// Directory prefix pattern (e.g. "bin/" matches "bin/foo")
		if strings.HasSuffix(pat, "/") && strings.HasPrefix(normalized, pat) {
			return true
		}
		// Basename match (pattern without slash matches any path with that basename)
		if !strings.Contains(pat, "/") {
			base := filepath.Base(normalized)
			matched, _ := filepath.Match(pat, base)
			if matched {
				return true
			}
		}
	}
	return false
}
