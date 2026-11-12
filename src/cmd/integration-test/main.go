// integration-test exercises the real InsulaVM boundary on an explicitly
// authorized KVM host. It is never launched by the unit-test CI workflow.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ahhvahh/InsulaVM/insulavm"
	actions "github.com/ahhvahh/gitmaster-actions"
)

func main() {
	var config, profile, iso, cache, db, yamlPath string
	flag.StringVar(&config, "config", "", "absolute InsulaVM config.yaml")
	flag.StringVar(&profile, "profile", "gitmaster-actions-test-500m", "test profile name")
	flag.StringVar(&iso, "iso", "", "optional local Debian 13 installer mini.iso")
	flag.StringVar(&cache, "cache-dir", "", "verified Debian ISO cache directory")
	flag.StringVar(&db, "db", "", "dedicated Actions SQLite path")
	flag.StringVar(&yamlPath, "yaml", "", "three-command YAML fixture")
	flag.Parse()
	if config == "" || db == "" || yamlPath == "" || cache == "" {
		fail(errors.New("--config, --db, --yaml and --cache-dir are required"))
	}
	if !filepath.IsAbs(config) || !filepath.IsAbs(db) || !filepath.IsAbs(yamlPath) {
		fail(errors.New("config, db and yaml paths must be absolute"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Hour)
	defer cancel()
	if err := run(ctx, config, profile, iso, cache, db, yamlPath); err != nil {
		fail(err)
	}
	fmt.Println("GITMASTER_ACTIONS_INTEGRATION_OK")
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "integration-test:", err)
	os.Exit(1)
}

func containsName(names []string, name string) bool {
	for _, n := range names {
		if n == name {
			return true
		}
	}
	return false
}

// prepareProfile does not accept an arbitrary ISO as an installed QCOW2.
// The InsulaVM installer itself installs Debian from an official ISO at its
// enforced minimum of 1024 MiB. A separate 500 MiB/1-vCPU profile is then
// registered by InsulaVM using the installed, validated QCOW2.
func prepareProfile(ctx context.Context, insula *insulavm.Engine, name, isoPath, cache string) error {
	if !regexp.MustCompile(`^[a-z][a-z0-9_-]{0,62}$`).MatchString(name) {
		return errors.New("invalid test profile name")
	}
	names, err := insula.ListProfiles()
	if err != nil {
		return err
	}
	if containsName(names, name) {
		p, err := insula.GetProfile(name)
		if err != nil {
			return err
		}
		if p.MemoryMiB != 500 || p.VCPUs != 1 || p.OS != "debian" {
			return fmt.Errorf("existing test profile %q is not Debian/500 MiB/1 vCPU", name)
		}
		if _, err := p.ImagePath(); err != nil {
			return err
		}
		fmt.Printf("[prepare] Reusing existing Debian profile %q: 500 MiB / 1 vCPU\n", name)
		return nil
	}

	installerName := name + "-installer"
	if !containsName(names, installerName) {
		if isoPath == "" {
			fmt.Println("[prepare] Downloading official Debian 13 mini.iso with SHA256 verification")
			isoPath, err = insulavm.DownloadDebianMiniISO(ctx, cache)
			if err != nil {
				return err
			}
		} else {
			if !filepath.IsAbs(isoPath) {
				return errors.New("--iso must be an absolute path")
			}
			info, err := os.Lstat(isoPath)
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return errors.New("--iso must be a regular installer ISO file")
			}
		}
		fmt.Printf("[prepare] Installing Debian from ISO using InsulaVM: %s\n", isoPath)
		_, err = insula.CreateProfileFromISO(ctx, insulavm.CreateProfileFromISORequest{
			Name:      installerName,
			ISOPath:   isoPath,
			MemoryMiB: 1024,
			VCPUs:     1,
			DiskGB:    10,
			Timeout:   110 * time.Minute,
			OnProgress: func(progress insulavm.ProfileISOProgress) {
				fmt.Printf("[install:%s] %s\n", progress.Stage, progress.Message)
			},
		})
		if err != nil {
			return fmt.Errorf("create installer profile: %w", err)
		}
	} else {
		fmt.Printf("[prepare] Reusing installed ISO-derived profile %q\n", installerName)
	}
	installer, err := insula.GetProfile(installerName)
	if err != nil {
		return err
	}
	if installer.OS != "debian" {
		return errors.New("installer profile is not Debian")
	}
	source, err := installer.ImagePath()
	if err != nil {
		return err
	}
	profile := *installer
	profile.Name = name
	profile.MemoryMiB = 500
	profile.VCPUs = 1
	profile.Image = ""
	profile.Version = ""
	profile.ISO = ""
	profile.Directory = ""
	if err := insula.RegisterProfile(profile, source); err != nil {
		return fmt.Errorf("register 500 MiB profile: %w", err)
	}
	fmt.Printf("[prepare] Registered profile %q: 500 MiB / 1 vCPU\n", name)
	return nil
}

func run(ctx context.Context, config, profile, isoPath, cache, db, yamlPath string) error {
	insula, err := insulavm.New(config)
	if err != nil {
		return fmt.Errorf("open InsulaVM config: %w", err)
	}
	if err := prepareProfile(ctx, insula, profile, isoPath, cache); err != nil {
		return err
	}

	data, err := os.ReadFile(yamlPath)
	if err != nil {
		return err
	}
	const placeholder = "__TEST_PROFILE__"
	if strings.Count(string(data), placeholder) != 1 {
		return errors.New("YAML fixture must contain exactly one __TEST_PROFILE__ placeholder")
	}
	// Use only names already validated by the InsulaVM profile API; YAML
	// injection cannot alter the three approved test commands.
	if !regexp.MustCompile(`^[a-z][a-z0-9_-]{0,62}$`).MatchString(profile) {
		return errors.New("unsupported YAML profile name")
	}
	data = []byte(strings.Replace(string(data), placeholder, profile, 1))
	a, err := actions.New(actions.Options{
		DatabasePath: db, InsulaConfigPath: config, Workers: 1,
	})
	if err != nil {
		return err
	}
	defer a.Close()
	fmt.Println("[run] Submitting exactly three YAML commands to an isolated VM")
	action, err := a.SubmitYAML(ctx, actions.SubmitRequest{YAML: data, Format: actions.FormatActionV1})
	if err != nil {
		return err
	}
	fmt.Printf("[run] action_run_id=%s (pending is not success)\n", action.ID)
	completed, err := a.WaitAction(ctx, action.ID)
	if err != nil {
		_ = a.CancelAction(context.Background(), action.ID)
		return err
	}
	details, err := a.GetActionDetails(ctx, action.ID)
	if err != nil {
		return err
	}
	fmt.Printf("[run] action=%s vm_id=%s cleanup=%s\n", completed.Status, details.VMID, details.CleanupStatus)
	if completed.Status != "succeeded" || details.CleanupStatus != "succeeded" || details.VMID == "" {
		return fmt.Errorf("action/cleanup incomplete: action=%s cleanup=%s error=%v", completed.Status, details.CleanupStatus, completed.ErrorMessage)
	}
	if len(details.Jobs) != 1 || len(details.Jobs[0].Commands) != 3 {
		return fmt.Errorf("expected exactly one job and three commands; got %d jobs", len(details.Jobs))
	}
	for i, c := range details.Jobs[0].Commands {
		expected := fmt.Sprintf("GM_STEP_%d_OK", i+1)
		if c.Command.Status != "succeeded" || c.Command.ExitCode == nil || *c.Command.ExitCode != 0 {
			return fmt.Errorf("command %d failed: status=%s exit_code=%v", i+1, c.Command.Status, c.Command.ExitCode)
		}
		outputs, err := a.ListCommandOutputs(ctx, c.Command.ID, actions.OutputFilter{StreamType: "stdout"}, actions.Page{Limit: 500})
		if err != nil {
			return err
		}
		var captured strings.Builder
		for _, out := range outputs {
			captured.Write(out.Payload)
		}
		if !strings.Contains(captured.String(), expected) {
			return fmt.Errorf("command %d missing expected marker %q; got %q", i+1, expected, captured.String())
		}
		fmt.Printf("[verify] command %d: status=success exit=0 output=%s\n", i+1, expected)
	}
	status, err := insula.GetVMStatus(ctx, details.VMID)
	if err != nil {
		return fmt.Errorf("read VM shutdown state: %w", err)
	}
	if status.LibvirtState != "absent" {
		return fmt.Errorf("VM %s was not removed: Libvirt state=%s", details.VMID, status.LibvirtState)
	}
	fmt.Printf("[verify] VM %s absent from Libvirt after StopVM\n", details.VMID)
	return nil
}