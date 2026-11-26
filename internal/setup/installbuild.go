package setup

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/statik/site-reliability-clanker/internal/pack"
)

// The architecture of the core, the runners and the nodes on AWS.
const imagePlatform = "linux/arm64"

// checkout is a checkout of this repository. A released program carries the platform
// files, but not the source of the runner and of the core, so the installer builds them
// from a checkout.
type checkout struct{ dir string }

func openCheckout(dir string) (checkout, error) {
	if dir == "" {
		return checkout{}, errors.New("give a checkout of S.R.C. (--source DIR, or the " +
			"field of the wizard): the runner image, the core and the application images " +
			"are built from it")
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return checkout{}, fmt.Errorf("resolve the checkout %s: %w", dir, err)
	}
	for _, name := range []string{"Makefile", "go.mod", "build/runner/Dockerfile", "apps"} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(name))); err != nil {
			return checkout{}, fmt.Errorf("%s is not a checkout of S.R.C.: it has no %s. "+
				"Give a checkout (--source DIR, or the field of the wizard)", dir, name)
		}
	}
	return checkout{dir: dir}, nil
}

// pack reads the application pack of the checkout: the one pack in apps, which the core
// that the checkout builds carries.
func (c checkout) pack() (pack.Pack, error) {
	files := os.DirFS(c.dir)
	dir, err := pack.Discover(files, "apps")
	if err != nil {
		return pack.Pack{}, err
	}
	return pack.Load(files, dir)
}

// release is the version of the checkout, as `make release` names it.
func (c checkout) release(ctx context.Context, p program) (string, error) {
	version, err := p.output(ctx, c.dir, "git", "describe", "--tags", "--always", "--dirty")
	if err != nil {
		return "", fmt.Errorf("name the release of the checkout %s: %w", c.dir, err)
	}
	if !releasePattern.MatchString(version) {
		return "", fmt.Errorf("the checkout %s has the release %q, which is not a valid name",
			c.dir, version)
	}
	return version, nil
}

func (r *installRun) source() checkout {
	dir, _ := filepath.Abs(r.in.Source)
	return checkout{dir: dir}
}

// repository splits the URL of a repository into the host of the registry and the name.
func repository(url string) (string, string) {
	host, name, _ := strings.Cut(url, "/")
	return host, name
}

// login gives Docker a token of the registry of the account.
func (r *installRun) login(ctx context.Context, host string) error {
	token, err := r.aws.Run(ctx, "ecr", "get-login-password", "--profile", r.aws.Profile,
		"--region", r.aws.Region)
	if err != nil {
		return fmt.Errorf("get a token of the registry: %w", err)
	}
	return r.program.run(ctx, "", nil, bytes.NewReader(bytes.TrimSpace(token)), "docker",
		"login", "--username", "AWS", "--password-stdin", host)
}

// heldDigest returns the digest of an image in a repository, or "" when it has no such
// image. The image is an image ID such as imageTag=v1 or imageDigest=sha256:...
func (r *installRun) heldDigest(ctx context.Context, name, image string) (string, error) {
	var digest string
	err := r.aws.query(ctx, r.aws.Region, &digest, "ecr", "describe-images",
		"--repository-name", name, "--image-ids", image, "--query",
		"imageDetails[0].imageDigest")
	if err != nil && strings.Contains(stderrOf(err), "ImageNotFoundException") {
		return "", nil
	}
	return digest, err
}

// pushRunner builds the runner image of the release for ARM64, pushes it to the repository
// of the stack and records its digest.
func (r *installRun) pushRunner(ctx context.Context) error {
	url := r.record.Outputs.RunnerRepository
	host, name := repository(url)
	if r.record.Runner.Release == r.release && r.record.Runner.Digest != "" {
		held, err := r.heldDigest(ctx, name, "imageDigest="+r.record.Runner.Digest)
		if err != nil {
			return err
		}
		if held != "" {
			r.log("The repository holds the runner image of %s: %s.", r.release, held)
			return nil
		}
	}
	source := r.source()
	if err := r.program.run(ctx, source.dir, nil, nil, "make", "bundle"); err != nil {
		return err
	}
	tag := url + ":" + r.release
	if err := r.program.run(ctx, source.dir, nil, nil, "docker", "buildx", "build",
		"--platform", imagePlatform, "--build-arg", "VERSION="+r.release, "--file",
		"build/runner/Dockerfile", "--tag", tag, "--load", "."); err != nil {
		return err
	}
	if err := r.login(ctx, host); err != nil {
		return err
	}
	if err := r.program.run(ctx, "", nil, nil, "docker", "push", tag); err != nil {
		return err
	}
	digest, err := r.heldDigest(ctx, name, "imageTag="+r.release)
	if err != nil {
		return err
	}
	if !digestPattern.MatchString(digest) {
		return fmt.Errorf("the repository %s has no image %s after the push", name, r.release)
	}
	r.record.Runner.Release, r.record.Runner.Digest = r.release, digest
	r.log("The runner image of %s is %s@%s.", r.release, url, digest)
	return r.save()
}

// pushApplication builds the images of every release of the application for ARM64 with
// the build program of its pack, and pushes them to image_registry. The core on a cloud
// does not publish images.
func (r *installRun) pushApplication(ctx context.Context) error {
	source, app := r.source(), r.app
	if app.Deploy.Build == "" {
		r.log("The pack %s has no build program: its images are in a registry already.",
			app.Name)
		return nil
	}
	registry := r.record.Outputs.ImageRegistry
	host, prefix := repository(registry)
	name := prefix + "/" + app.Name
	if r.record.Application == r.release {
		var count int
		if err := r.aws.query(ctx, r.aws.Region, &count, "ecr", "list-images",
			"--repository-name", name, "--query", "length(imageIds)"); err != nil {
			return err
		}
		if count > 0 {
			r.log("The repository %s holds the images of the application of %s.", name,
				r.release)
			return nil
		}
	}
	if err := r.login(ctx, host); err != nil {
		return err
	}
	packDir := filepath.Join(source.dir, filepath.FromSlash(app.Dir()))
	build := filepath.Join(packDir, filepath.FromSlash(app.Deploy.Build))
	if err := r.program.run(ctx, packDir, nil, nil, build, "--platform", imagePlatform,
		registry); err != nil {
		return err
	}
	r.record.Application = r.release
	return r.save()
}

// currentImage returns the recorded image of the core for the release and the kernel.
func (r *installRun) currentImage() (recordedImage, bool) {
	for _, image := range r.record.Images {
		if image.Release == r.release && image.Nanos == r.in.Nanos {
			return image, true
		}
	}
	return recordedImage{}, false
}

// buildCore builds the binary of the core in the build of the runner image and makes the
// machine image of the core from it with ops.
func (r *installRun) buildCore(ctx context.Context) error {
	if image, found := r.currentImage(); found {
		held, err := r.aws.imagesNamed(ctx, image.Name)
		if err != nil {
			return err
		}
		if len(held) == 1 && held[0].ID == image.ID {
			r.log("The image %s of %s with Nanos %s exists: %s.", image.Name, r.release,
				r.in.Nanos, image.ID)
			return nil
		}
	}
	if err := r.removeImageOfRelease(ctx); err != nil {
		return err
	}
	if !maps.Equal(r.record.Outputs.CoreImageEnv, r.aws.opsConfig(ImageBuild{}).Env) {
		return fmt.Errorf("the home stack gives the image of the core the settings %v, and "+
			"the build would write others", slices.Sorted(maps.Keys(r.record.Outputs.CoreImageEnv)))
	}
	build := r.home.build()
	if err := os.MkdirAll(build, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", build, err)
	}
	if err := r.program.run(ctx, r.source().dir, nil, nil, "docker", "buildx", "build",
		"--platform", imagePlatform, "--build-arg", "VERSION="+r.release, "--file",
		"build/runner/Dockerfile", "--target", "core", "--output",
		"type=local,dest="+build, "."); err != nil {
		return err
	}
	r.log("Building the image with ops and the kernel of Nanos %s. This takes some minutes.",
		r.in.Nanos)
	image, err := r.aws.BuildImage(ctx, ImageBuild{Binary: filepath.Join(build, "clanker"),
		Release: r.release, Nanos: r.in.Nanos, Zone: r.zone,
		Bucket: "clanker-" + r.in.Env + "-ops", Dir: build})
	if image.ID != "" {
		r.record.Images = append(r.record.Images, recordedImage{Image: image, Release: r.release,
			Nanos: r.in.Nanos})
		if saveErr := r.save(); saveErr != nil {
			return errors.Join(err, saveErr)
		}
	}
	if err != nil {
		return err
	}
	r.log("The image of the core is %s, with the snapshot %s.", image.ID, image.SnapshotID)
	return nil
}

// removeImageOfRelease removes an image with the name of the release, which BuildImage
// would refuse: one with another kernel, or one of a run that stopped before it was
// recorded.
func (r *installRun) removeImageOfRelease(ctx context.Context) error {
	name := r.aws.imageName(r.release)
	held, err := r.aws.imagesNamed(ctx, name)
	if err != nil {
		return err
	}
	for _, image := range held {
		r.log("Removing the image %s (%s) of an earlier build, so that the image of Nanos "+
			"%s can take its name.", image.ID, name, r.in.Nanos)
		if err := r.aws.RemoveImage(ctx, image); err != nil {
			return err
		}
	}
	kept := r.record.Images[:0]
	for _, image := range r.record.Images {
		if image.Name != name {
			kept = append(kept, image)
		}
	}
	r.record.Images = kept
	return r.save()
}
