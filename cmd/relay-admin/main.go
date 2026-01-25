package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ngoyal88/relay/pkg/cache"
	"github.com/ngoyal88/relay/pkg/config"
	"github.com/ngoyal88/relay/pkg/keymanager"
	"github.com/ngoyal88/relay/pkg/middleware"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	switch cmd {
	case "init":
		adminKey, err := generateAdminKey()
		if err != nil {
			log.Fatalf("failed to generate admin key: %v", err)
		}
		if err := writeAdminKey(adminKey); err != nil {
			log.Fatalf("failed to write .env: %v", err)
		}
		fmt.Printf("AdminKey: %s\nSaved to .env (ADMIN_KEY).\n", adminKey)
	case "create-key":
		cfg := mustLoadConfig()
		rdb := mustRedis(cfg)
		handleCreateKey(cfg, rdb)
	case "list-keys":
		cfg := mustLoadConfig()
		rdb := mustRedis(cfg)
		handleListKeys(rdb)
	default:
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Println("relay-admin commands:")
	fmt.Println("  init                 Generate admin key and store in .env")
	fmt.Println("  create-key           Create a new API key")
	fmt.Println("     flags: -name -user -desc -rps -burst -quota -expires-days")
	fmt.Println("  list-keys            List all active keys")
}

func mustLoadConfig() *config.Config {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}
	return cfg
}

func mustRedis(cfg *config.Config) *cache.Client {
	if cfg == nil || !cfg.Redis.Enabled {
		log.Fatal("redis is not enabled in config")
	}
	rdb, err := cache.NewRedis(cfg.Redis.Address, cfg.Redis.Password, cfg.Redis.DB)
	if err != nil {
		log.Fatalf("failed to connect redis: %v", err)
	}
	return rdb
}

func generateAdminKey() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "admin_" + base64.RawURLEncoding.EncodeToString(b), nil
}

func writeAdminKey(adminKey string) error {
	const envFile = ".env"
	var lines []string

	data, err := os.ReadFile(envFile)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		lines = []string{fmt.Sprintf("ADMIN_KEY=%s", adminKey)}
		return os.WriteFile(envFile, []byte(strings.Join(lines, "\n")+"\n"), 0644)
	}

	lines = strings.Split(string(data), "\n")
	replaced := false
	for i, line := range lines {
		if strings.HasPrefix(line, "ADMIN_KEY=") {
			lines[i] = fmt.Sprintf("ADMIN_KEY=%s", adminKey)
			replaced = true
			break
		}
	}
	if !replaced {
		lines = append(lines, fmt.Sprintf("ADMIN_KEY=%s", adminKey))
	}

	content := strings.Join(lines, "\n")
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return os.WriteFile(envFile, []byte(content), 0644)
}

func handleCreateKey(cfg *config.Config, rdb *cache.Client) {
	fs := flag.NewFlagSet("create-key", flag.ExitOnError)
	name := fs.String("name", "root", "Key name")
	user := fs.String("user", "root", "User ID")
	desc := fs.String("desc", "bootstrap key", "Description")
	rps := fs.Float64("rps", 10, "Requests per second")
	burst := fs.Int("burst", 20, "Burst")
	quota := fs.Int64("quota", 0, "Quota (0 = unlimited)")
	expiresDays := fs.Int("expires-days", 0, "Expires in N days (0 = never)")

	if err := fs.Parse(os.Args[2:]); err != nil {
		log.Fatalf("failed to parse flags: %v", err)
	}

	var expiresIn *time.Duration
	if *expiresDays > 0 {
		d := time.Duration(*expiresDays) * 24 * time.Hour
		expiresIn = &d
	}

	km := keymanager.New(rdb)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	key, err := km.CreateKey(ctx, *name, *user, *desc, *rps, *burst, *quota, expiresIn)
	if err != nil {
		log.Fatalf("failed to create key: %v", err)
	}

	b, _ := json.MarshalIndent(key, "", "  ")
	fmt.Println(string(b))
}

func handleListKeys(rdb *cache.Client) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	iter := rdb.Redis().Scan(ctx, 0, "apikey:*", 100).Iterator()
	count := 0
	for iter.Next(ctx) {
		data, err := rdb.Get(ctx, iter.Val())
		if err != nil {
			continue
		}
		var k middleware.APIKey
		if err := json.Unmarshal(data, &k); err != nil {
			continue
		}
		if !k.Active {
			continue
		}
		count++
		fmt.Printf("%d) %s user=%s created=%s used=%d quota=%d expires=%v\n",
			count, k.Key, k.UserID, k.CreatedAt.Format(time.RFC3339), k.Used, k.Quota, k.ExpiresAt)
	}

	if err := iter.Err(); err != nil {
		log.Fatalf("scan error: %v", err)
	}

	if count == 0 {
		fmt.Println("No active keys found")
	}
}

// resolveRepoRoot returns absolute path of repo root when invoked from nested dir.
// Currently unused but kept for potential future needs.
func resolveRepoRoot() string {
	cwd, _ := os.Getwd()
	return filepath.Clean(cwd)
}
