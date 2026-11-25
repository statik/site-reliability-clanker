package setup

import (
	"bytes"
	"context"
	"debug/elf"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// coreFlavor is the instance type of the core on AWS. ops takes the architecture of the
// machine image and the UEFI boot from it. parameterPathEnv names the setting with the path
// of the settings in Parameter Store, which cmd/clanker reads.
const (
	coreFlavor       = "t4g.small"
	coreAddr         = "0.0.0.0:7070"
	parameterPathEnv = "CLANKER_PARAMETER_PATH"
)

var (
	releasePattern      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	nanosVersionPattern = regexp.MustCompile(`^\d+\.\d+\.\d+$`)
	bucketPattern       = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)
	imageIDPattern      = regexp.MustCompile(`^ami-[0-9a-f]{8,17}$`)
	snapshotIDPattern   = regexp.MustCompile(`^snap-[0-9a-f]{8,17}$`)
)

// parameterPath is the path in Parameter Store below which the core on AWS finds its
// settings.
func parameterPath(env string) string {
	return "/src/" + env
}

// ImageBuild is what the build of the machine image of the core on AWS needs.
type ImageBuild struct {
	// Binary is the linux/arm64 binary of `make release`, which carries its source files.
	Binary string
	// Release is the version of the binary. The image is named clanker-<env>-<release>.
	Release string
	// Nanos is the version of the Nanos release whose kernel the image boots, for example
	// 0.1.55. ops gets it as --nanos-version; the image never has the nightly kernel. The
	// arm64 kernel of 0.1.56 did not boot on an Apple CPU under HVF, because its early boot
	// uses DC ZVA while the MMU is off (see ops/run-local.sh).
	Nanos string
	// Zone is an availability zone of the region.
	Zone string
	// Bucket is a bucket name. ops 0.1.47 refuses a configuration without one, but it
	// writes the snapshot with the EBS direct API and does not use the bucket.
	Bucket string
	// Dir is the directory that receives the configuration of ops.
	Dir string
}

// Image is a machine image of the core with its snapshot. The installer records it, so
// that the removal finds both.
type Image struct {
	// Name is the value of the tag Name. ops adds a time to it for the name of the image.
	Name       string `json:"name"`
	ID         string `json:"id"`
	SnapshotID string `json:"snapshot_id"`
}

// opsTag, opsCloudConfig and opsConfig are the part of the configuration file of ops 0.1.47
// that the build writes. ops refuses a field that it does not know.
type opsTag struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type opsCloudConfig struct {
	Zone       string   `json:"Zone"`
	Flavor     string   `json:"Flavor"`
	BucketName string   `json:"BucketName"`
	ImageName  string   `json:"ImageName"`
	Tags       []opsTag `json:"Tags"`
}

type opsConfig struct {
	CloudConfig opsCloudConfig    `json:"CloudConfig"`
	Args        []string          `json:"Args"`
	Env         map[string]string `json:"Env"`
}

// imageName is the value of the tag Name of the machine image of a release.
func (a AWS) imageName(release string) string {
	return "clanker-" + a.Env + "-" + release
}

// opsConfig returns the configuration of ops for the image. ops writes Env into the image,
// so Env holds only the path of the settings and the region of their store, never a secret.
// ops puts the tags on the snapshot and on the image when it makes them, so a build that
// stops after the snapshot leaves one that carries the tag of the environment.
func (a AWS) opsConfig(build ImageBuild) opsConfig {
	return opsConfig{
		CloudConfig: opsCloudConfig{Zone: build.Zone, Flavor: coreFlavor,
			BucketName: build.Bucket, ImageName: a.imageName(build.Release),
			Tags: []opsTag{{Key: planTagKey, Value: a.Env}}},
		Args: []string{"up", "--cloud", "aws", "--env", a.Env, "--demo=false",
			"--addr", coreAddr},
		Env: map[string]string{parameterPathEnv: parameterPath(a.Env), "AWS_REGION": a.Region},
	}
}

func (a AWS) validateBuild(build ImageBuild) error {
	if err := a.Validate(); err != nil {
		return err
	}
	if !releasePattern.MatchString(build.Release) {
		return fmt.Errorf("the release %q is not valid: use letters, digits, '.', '_' and "+
			"'-', for example v0.3.0", build.Release)
	}
	if !nanosVersionPattern.MatchString(build.Nanos) {
		return fmt.Errorf("the Nanos version %q is not valid: use the version of a release, "+
			"for example 0.1.55", build.Nanos)
	}
	zone := regexp.MustCompile(`^` + regexp.QuoteMeta(a.Region) + `[a-z]$`)
	if !zone.MatchString(build.Zone) {
		return fmt.Errorf("the zone %q is not an availability zone of %s: use a name such "+
			"as %sa", build.Zone, a.Region, a.Region)
	}
	if !bucketPattern.MatchString(build.Bucket) {
		return fmt.Errorf("the bucket name %q is not valid: use 3 to 63 lowercase letters, "+
			"digits, '.' or '-'", build.Bucket)
	}
	if build.Dir == "" {
		return errors.New("the directory for the configuration of ops is not set")
	}
	return checkARM64(build.Binary)
}

// checkARM64 fails unless the file is a 64-bit ELF program for arm64, the architecture
// of the instance type.
func checkARM64(path string) error {
	file, err := elf.Open(path)
	if err != nil {
		return fmt.Errorf("read the binary %s: %w. Build it with `make release` and give "+
			"dist/clanker-linux-arm64", path, err)
	}
	defer file.Close()
	if file.Class != elf.ELFCLASS64 || file.Machine != elf.EM_AARCH64 {
		return fmt.Errorf("the binary %s is for %s, not for arm64: give "+
			"dist/clanker-linux-arm64", path, file.Machine)
	}
	return nil
}

// findOps returns the path of the ops program: the one in PATH, else the one that the
// installer of ops puts in ~/.ops/bin.
func findOps() (string, error) {
	if path, err := LookPath("ops"); err == nil {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err == nil {
		path := filepath.Join(home, ".ops", "bin", "ops")
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			return path, nil
		}
	}
	return "", errors.New("ops is not in PATH or in ~/.ops/bin: install it from https://ops.city")
}

// opsEnvironment returns the environment of ops: this one, with the profile and the
// region of the survey. Keys in the environment would win over the profile in ops, which
// would then build the image in another account than the one that the survey read.
func opsEnvironment(base []string, profile, region string) []string {
	env := make([]string, 0, len(base)+2)
	for _, entry := range base {
		name, _, _ := strings.Cut(entry, "=")
		switch name {
		case "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN", "AWS_PROFILE",
			"AWS_DEFAULT_PROFILE", "AWS_REGION", "AWS_DEFAULT_REGION":
			continue
		}
		env = append(env, entry)
	}
	return append(env, "AWS_PROFILE="+profile, "AWS_REGION="+region)
}

// BuildImage builds the machine image of the core with ops, from the release kernel of
// Nanos, tags the image and its snapshot with the environment and returns both. It refuses
// a release whose image exists already. When only the tag fails, the returned Image names
// what ops made, so that the caller can record it for the removal.
func (a AWS) BuildImage(ctx context.Context, build ImageBuild) (Image, error) {
	if err := a.validateBuild(build); err != nil {
		return Image{}, err
	}
	program, err := findOps()
	if err != nil {
		return Image{}, err
	}
	name := a.imageName(build.Release)
	existing, err := a.imagesNamed(ctx, name)
	if err != nil {
		return Image{}, err
	}
	if len(existing) > 0 {
		return Image{}, fmt.Errorf("the image %s exists already as %s: remove it, or build "+
			"another release", name, existing[0].ID)
	}
	output, err := a.runOps(ctx, program, build)
	if err != nil {
		return Image{}, err
	}
	image, err := a.findImage(ctx, name)
	if err != nil {
		// ops 0.1.47 can end with the status 0 after an error, so its output says why.
		return Image{}, fmt.Errorf("%w. The output of ops ends with: %s", err, output)
	}
	if _, err := a.cli(ctx, "ec2", "create-tags", "--resources", image.ID, image.SnapshotID,
		"--tags", "Key="+planTagKey+",Value="+a.Env); err != nil {
		return image, fmt.Errorf("tag the image %s and the snapshot %s: %w", image.ID,
			image.SnapshotID, err)
	}
	return image, nil
}

// runOps writes the configuration of ops into the directory of the build and runs ops
// image create with it. It returns the end of the output of ops.
func (a AWS) runOps(ctx context.Context, program string, build ImageBuild) (string, error) {
	name := a.imageName(build.Release)
	config, err := json.MarshalIndent(a.opsConfig(build), "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode the configuration of ops: %w", err)
	}
	configPath := filepath.Join(build.Dir, name+".ops.json")
	if err := os.WriteFile(configPath, append(config, '\n'), 0o600); err != nil {
		return "", fmt.Errorf("write the configuration of ops: %w", err)
	}
	args := []string{"image", "create", build.Binary, "-t", "aws", "-c", configPath,
		"--arch", "arm64", "--nanos-version", build.Nanos}
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Env = opsEnvironment(os.Environ(), a.Profile, a.Region)
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("ops image create %s: %w: %s. A snapshot or an image can "+
			"remain: look for the tag %s=%s", name, err, lastPart(output.String()), planTagKey,
			a.Env)
	}
	return lastPart(output.String()), nil
}

// imagesNamed returns the images of the account in the region that ops made with the tag
// Name.
func (a AWS) imagesNamed(ctx context.Context, name string) ([]Image, error) {
	var found []struct {
		ID       string `json:"id"`
		Snapshot string `json:"snapshot"`
	}
	if err := a.query(ctx, a.Region, &found, "ec2", "describe-images", "--owners", "self",
		"--filters", "Name=tag:Name,Values="+name, "Name=tag:CreatedBy,Values=ops",
		"--query", "Images[].{id:ImageId,snapshot:BlockDeviceMappings[0].Ebs.SnapshotId}",
	); err != nil {
		return nil, fmt.Errorf("look for the image %s: %w", name, err)
	}
	images := make([]Image, 0, len(found))
	for _, one := range found {
		images = append(images, Image{Name: name, ID: one.ID, SnapshotID: one.Snapshot})
	}
	return images, nil
}

// findImage returns the image that ops made with the tag Name and its snapshot.
func (a AWS) findImage(ctx context.Context, name string) (Image, error) {
	images, err := a.imagesNamed(ctx, name)
	if err != nil {
		return Image{}, err
	}
	if len(images) != 1 {
		return Image{}, fmt.Errorf("ops finished, but %d images in %s have the tag Name=%s, "+
			"not one", len(images), a.Region, name)
	}
	image := images[0]
	if !imageIDPattern.MatchString(image.ID) || !snapshotIDPattern.MatchString(image.SnapshotID) {
		return Image{}, fmt.Errorf("the image %s has the ID %q and the snapshot %q, which "+
			"are not valid", name, image.ID, image.SnapshotID)
	}
	return image, nil
}

// RemoveImage deregisters the image and then deletes its snapshot. A part that is gone
// already is skipped, so a removal that stopped halfway can run again. It refuses an image
// whose tag Name is not the recorded one.
func (a AWS) RemoveImage(ctx context.Context, image Image) error {
	if err := a.Validate(); err != nil {
		return err
	}
	if !imageIDPattern.MatchString(image.ID) || !snapshotIDPattern.MatchString(image.SnapshotID) {
		return fmt.Errorf("the record of the image is not valid: image %q, snapshot %q",
			image.ID, image.SnapshotID)
	}
	var images []struct {
		Name string `json:"name"`
	}
	if err := a.query(ctx, a.Region, &images, "ec2", "describe-images", "--owners", "self",
		"--filters", "Name=image-id,Values="+image.ID,
		"--query", "Images[].{name:Tags[?Key=='Name']|[0].Value}"); err != nil {
		return fmt.Errorf("look for the image %s: %w", image.ID, err)
	}
	if len(images) > 0 {
		if images[0].Name != image.Name {
			return fmt.Errorf("the image %s has the tag Name=%q, not %q: the record does not "+
				"match it, so nothing was removed", image.ID, images[0].Name, image.Name)
		}
		if _, err := a.cli(ctx, "ec2", "deregister-image", "--image-id", image.ID); err != nil {
			return fmt.Errorf("deregister the image %s: %w", image.ID, err)
		}
	}
	var snapshots []string
	if err := a.query(ctx, a.Region, &snapshots, "ec2", "describe-snapshots", "--owner-ids",
		"self", "--filters", "Name=snapshot-id,Values="+image.SnapshotID,
		"--query", "Snapshots[].SnapshotId"); err != nil {
		return fmt.Errorf("look for the snapshot %s: %w", image.SnapshotID, err)
	}
	if len(snapshots) == 0 {
		return nil
	}
	if _, err := a.cli(ctx, "ec2", "delete-snapshot", "--snapshot-id",
		image.SnapshotID); err != nil {
		return fmt.Errorf("delete the snapshot %s of the image %s: %w", image.SnapshotID,
			image.ID, err)
	}
	return nil
}
