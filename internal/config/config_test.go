package config

import (
	"os"
	"testing"
)

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid configuration",
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
			},
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
				os.Setenv(tt.key, tt.envValue)
				defer os.Unsetenv(tt.key)
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
				os.Setenv(tt.key, tt.envValue)
				defer os.Unsetenv(tt.key)
			}

			got := getEnvAsInt(tt.key, tt.defaultValue)
			if got != tt.want {
				t.Errorf("getEnvAsInt() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLoad_DefaultValues(t *testing.T) {
	// This test verifies that Load() returns valid defaults
	// Note: It may load values from .env file if present
	cfg := Load()

	// Verify configuration is valid
	if err := cfg.Validate(); err != nil {
		t.Errorf("Loaded config should be valid, got error: %v", err)
	}

	// Verify numeric defaults are reasonable (not testing exact values
	// since .env file might exist and override them)
	if cfg.DBMaxIdleConns < 1 {
		t.Errorf("Expected DBMaxIdleConns to be at least 1, got %d", cfg.DBMaxIdleConns)
	}
	if cfg.DBMaxOpenConns < 1 {
		t.Errorf("Expected DBMaxOpenConns to be at least 1, got %d", cfg.DBMaxOpenConns)
	}
	if cfg.APIBatchMaxSize < 1 {
		t.Errorf("Expected APIBatchMaxSize to be at least 1, got %d", cfg.APIBatchMaxSize)
	}
	if cfg.APISearchMaxLimit < 1 {
		t.Errorf("Expected APISearchMaxLimit to be at least 1, got %d", cfg.APISearchMaxLimit)
	}
	if cfg.APISuggestMaxLimit < 1 {
		t.Errorf("Expected APISuggestMaxLimit to be at least 1, got %d", cfg.APISuggestMaxLimit)
	}

	// Verify required fields are not empty
	if cfg.DBHost == "" {
		t.Error("DBHost should not be empty")
	}
	if cfg.DBName == "" {
		t.Error("DBName should not be empty")
	}
	if cfg.Port == "" {
		t.Error("Port should not be empty")
	}
}
