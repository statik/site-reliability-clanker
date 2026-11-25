package setup

import (
	"bytes"
	"context"
	"debug/elf"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// The stand-ins answer from files in $FAKE_DIR, so that each test says what AWS and ops
// do. An answer can change once ops has run. The stand-ins log each call there. Neither one
// calls anything.
const fakeAWS = `#!/usr/bin/env bash
set -euo pipefail
echo "$*" >>"${FAKE_DIR}/aws.calls"
answer="${FAKE_DIR}/aws-$1-$2"
if [ -f "${FAKE_DIR}/ops.args" ] && [ -f "${answer}.after-ops.json" ]; then
  cat "${answer}.after-ops.json"
  exit 0
fi
if [ -f "${answer}.fail" ]; then
  cat "${answer}.fail" >&2
  exit 254
fi
if [ -f "${answer}.json" ]; then
  cat "${answer}.json"
  exit 0
fi
echo "the stand-in has no answer for: aws $*" >&2
exit 1
`

const fakeOps = `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$@" >"${FAKE_DIR}/ops.args"
{
  echo "AWS_PROFILE=${AWS_PROFILE:-}"
  echo "AWS_REGION=${AWS_REGION:-}"
  echo "AWS_ACCESS_KEY_ID=${AWS_ACCESS_KEY_ID:-}"
} >"${FAKE_DIR}/ops.env"
config=""
while [ $# -gt 0 ]; do
  if [ "$1" = "-c" ]; then config="$2"; fi
  shift
done
cp "${config}" "${FAKE_DIR}/ops.config"
if [ -f "${FAKE_DIR}/ops.fail" ]; then
  cat "${FAKE_DIR}/ops.fail" >&2
  exit 1
fi
if [ -f "${FAKE_DIR}/ops.says" ]; then
  cat "${FAKE_DIR}/ops.says"
fi
`

const (
	testImageID    = "ami-0fa1e2d3c4b5a6978"
	testSnapshotID = "snap-0a1b2c3d4e5f60718"
)

// stage is the directory of the stand-ins of one test.
type stage struct {
	t   *testing.T
	dir string
}

// newStage puts the stand-ins first in PATH. Without ops, PATH and HOME hold no ops.
func newStage(t *testing.T, withOps bool) stage {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(bin, "aws"), fakeAWS, 0o755)
	if withOps {
		write(t, filepath.Join(bin, "ops"), fakeOps, 0o755)
	}
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	t.Setenv("HOME", dir)
	t.Setenv("FAKE_DIR", dir)
	return stage{t: t, dir: dir}
}

func write(t *testing.T, path, text string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), mode); err != nil {
		t.Fatal(err)
	}
}

// answer makes `aws <service> <operation>` print the text.
func (s stage) answer(service, operation, text string) {
	write(s.t, filepath.Join(s.dir, "aws-"+service+"-"+operation+".json"), text, 0o644)
}

// answerAfterOps makes `aws <service> <operation>` print the text once ops has run.
func (s stage) answerAfterOps(service, operation, text string) {
	write(s.t, filepath.Join(s.dir, "aws-"+service+"-"+operation+".after-ops.json"), text,
		0o644)
}

// refuse makes `aws <service> <operation>` fail with the text.
func (s stage) refuse(service, operation, text string) {
	write(s.t, filepath.Join(s.dir, "aws-"+service+"-"+operation+".fail"), text, 0o644)
}

func (s stage) read(name string) string {
	s.t.Helper()
	data, err := os.ReadFile(filepath.Join(s.dir, name))
	if err != nil {
		s.t.Fatalf("read %s of the stand-ins: %v", name, err)
	}
	return string(data)
}

// awsCalls returns the calls of the aws stand-in without the options that every call has.
func (s stage) awsCalls() []string {
	data, err := os.ReadFile(filepath.Join(s.dir, "aws.calls"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		s.t.Fatal(err)
	}
	calls := []string{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		const common = " --profile fixture --region us-west-2 --output json"
		if !strings.HasSuffix(line, common) {
			s.t.Fatalf("the call %q lacks the profile, the region or the output", line)
		}
		calls = append(calls, strings.TrimSuffix(line, common))
	}
	return calls
}

// writeELF writes the header of a 64-bit ELF program for the machine.
func writeELF(t *testing.T, path string, machine elf.Machine) {
	t.Helper()
	header := elf.Header64{Type: uint16(elf.ET_EXEC), Machine: uint16(machine),
		Version: uint32(elf.EV_CURRENT), Ehsize: 64, Phentsize: 56, Shentsize: 64}
	copy(header.Ident[:], elf.ELFMAG)
	header.Ident[elf.EI_CLASS] = byte(elf.ELFCLASS64)
	header.Ident[elf.EI_DATA] = byte(elf.ELFDATA2LSB)
	header.Ident[elf.EI_VERSION] = byte(elf.EV_CURRENT)
	var buf bytes.Buffer
	if err := binary.Write(&buf, binary.LittleEndian, header); err != nil {
		t.Fatal(err)
	}
	write(t, path, buf.String(), 0o755)
}

func testAWS() AWS {
	return AWS{Profile: "fixture", Region: "us-west-2", Env: "prod", Run: realCLI}
}

func testBuild(t *testing.T, s stage) ImageBuild {
	t.Helper()
	binaryPath := filepath.Join(s.dir, "clanker-linux-arm64")
	writeELF(t, binaryPath, elf.EM_AARCH64)
	return ImageBuild{Binary: binaryPath, Release: "v0.3.0", Nanos: "0.1.55",
		Zone: "us-west-2a", Bucket: "unused-ops-bucket", Dir: s.dir}
}

const builtImage = `[{"id":"` + testImageID + `","snapshot":"` + testSnapshotID + `"}]`

// opsMakesTheImage answers the look-up for the image with nothing before ops runs and with
// the image after it.
func (s stage) opsMakesTheImage() {
	s.answer("ec2", "describe-images", "[]")
	s.answerAfterOps("ec2", "describe-images", builtImage)
}

const lookForBuiltImage = "ec2 describe-images --owners self --filters " +
	"Name=tag:Name,Values=clanker-prod-v0.3.0 Name=tag:CreatedBy,Values=ops --query " +
	"Images[].{id:ImageId,snapshot:BlockDeviceMappings[0].Ebs.SnapshotId}"

func TestBuildImageRunsOpsWithTheReleaseKernelAndTagsTheImage(t *testing.T) {
	s := newStage(t, true)
	t.Setenv("AWS_ACCESS_KEY_ID", "AKIAFIXTUREKEY")
	t.Setenv("AWS_PROFILE", "other")
	s.opsMakesTheImage()
	s.answer("ec2", "create-tags", "")
	build := testBuild(t, s)

	image, err := testAWS().BuildImage(context.Background(), build)
	if err != nil {
		t.Fatal(err)
	}
	want := Image{Name: "clanker-prod-v0.3.0", ID: testImageID, SnapshotID: testSnapshotID}
	if image != want {
		t.Fatalf("image = %+v, want %+v", image, want)
	}

	configPath := filepath.Join(s.dir, "clanker-prod-v0.3.0.ops.json")
	args := strings.Fields(s.read("ops.args"))
	wantArgs := []string{"image", "create", build.Binary, "-t", "aws", "-c", configPath,
		"--arch", "arm64", "--nanos-version", "0.1.55"}
	if !slices.Equal(args, wantArgs) {
		t.Fatalf("ops arguments = %q, want %q", args, wantArgs)
	}
	if slices.Contains(args, "-n") || slices.Contains(args, "--nightly") {
		t.Fatalf("ops got the nightly kernel: %q", args)
	}
	env := s.read("ops.env")
	if env != "AWS_PROFILE=fixture\nAWS_REGION=us-west-2\nAWS_ACCESS_KEY_ID=\n" {
		t.Fatalf("environment of ops = %q, want the profile and region of the survey "+
			"and no key", env)
	}

	wantCalls := []string{
		lookForBuiltImage,
		lookForBuiltImage,
		"ec2 create-tags --resources " + testImageID + " " + testSnapshotID +
			" --tags Key=clanker_env,Value=prod",
	}
	if calls := s.awsCalls(); !slices.Equal(calls, wantCalls) {
		t.Fatalf("aws calls:\n%s\nwant:\n%s", strings.Join(calls, "\n"),
			strings.Join(wantCalls, "\n"))
	}
}

func TestBuildImageWritesAConfigurationWithoutASecret(t *testing.T) {
	s := newStage(t, true)
	s.opsMakesTheImage()
	s.answer("ec2", "create-tags", "")
	if _, err := testAWS().BuildImage(context.Background(), testBuild(t, s)); err != nil {
		t.Fatal(err)
	}

	decoder := json.NewDecoder(strings.NewReader(s.read("ops.config")))
	decoder.DisallowUnknownFields()
	var config opsConfig
	if err := decoder.Decode(&config); err != nil {
		t.Fatalf("decode the configuration that ops got: %v", err)
	}
	wantCloud := opsCloudConfig{Zone: "us-west-2a", Flavor: "t4g.small",
		BucketName: "unused-ops-bucket", ImageName: "clanker-prod-v0.3.0",
		Tags: []opsTag{{Key: "clanker_env", Value: "prod"}}}
	if !reflect.DeepEqual(config.CloudConfig, wantCloud) {
		t.Fatalf("CloudConfig = %+v, want %+v", config.CloudConfig, wantCloud)
	}
	wantArgs := []string{"up", "--cloud", "aws", "--env", "prod", "--demo=false",
		"--addr", "0.0.0.0:7070"}
	if !slices.Equal(config.Args, wantArgs) {
		t.Fatalf("Args = %q, want %q", config.Args, wantArgs)
	}
	wantEnv := map[string]string{"CLANKER_PARAMETER_PATH": "/src/prod", "AWS_REGION": "us-west-2"}
	if !reflect.DeepEqual(config.Env, wantEnv) {
		t.Fatalf("Env = %v, want only the parameter path and the region", config.Env)
	}
}

func TestBuildImageRefusesInputsBeforeItRunsAnything(t *testing.T) {
	tests := []struct {
		name   string
		change func(*AWS, *ImageBuild)
		want   string
	}{
		{"nightly kernel", func(_ *AWS, b *ImageBuild) { b.Nanos = "nightly" }, "Nanos version"},
		{"no kernel", func(_ *AWS, b *ImageBuild) { b.Nanos = "" }, "Nanos version"},
		{"release with a space", func(_ *AWS, b *ImageBuild) { b.Release = "v1 x" }, "release"},
		{"zone of another region", func(_ *AWS, b *ImageBuild) { b.Zone = "us-east-1a" },
			"not an availability zone of us-west-2"},
		{"bucket name", func(_ *AWS, b *ImageBuild) { b.Bucket = "Bad_Bucket" }, "bucket name"},
		{"environment name", func(a *AWS, _ *ImageBuild) { a.Env = "Prod" }, "environment name"},
		{"missing binary", func(_ *AWS, b *ImageBuild) { b.Binary += ".missing" },
			"read the binary"},
		{"no directory", func(_ *AWS, b *ImageBuild) { b.Dir = "" }, "directory"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := newStage(t, true)
			a, build := testAWS(), testBuild(t, s)
			test.change(&a, &build)
			_, err := a.BuildImage(context.Background(), build)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want one about %q", err, test.want)
			}
			if _, err := os.Stat(filepath.Join(s.dir, "ops.args")); err == nil {
				t.Fatal("ops ran")
			}
			if calls := s.awsCalls(); len(calls) != 0 {
				t.Fatalf("aws ran: %q", calls)
			}
		})
	}
}

func TestBuildImageRefusesABinaryForAnotherArchitecture(t *testing.T) {
	s := newStage(t, true)
	build := testBuild(t, s)
	writeELF(t, build.Binary, elf.EM_X86_64)
	_, err := testAWS().BuildImage(context.Background(), build)
	if err == nil || !strings.Contains(err.Error(), "not for arm64") {
		t.Fatalf("error = %v, want a refusal of an x86-64 binary", err)
	}
}

func TestBuildImageWithoutOpsSaysWhereToGetIt(t *testing.T) {
	s := newStage(t, false)
	_, err := testAWS().BuildImage(context.Background(), testBuild(t, s))
	if err == nil || !strings.Contains(err.Error(), "https://ops.city") {
		t.Fatalf("error = %v, want the place to install ops", err)
	}
}

func TestBuildImageFindsOpsInItsOwnDirectory(t *testing.T) {
	s := newStage(t, false)
	if err := os.MkdirAll(filepath.Join(s.dir, ".ops", "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(s.dir, ".ops", "bin", "ops"), fakeOps, 0o755)
	s.opsMakesTheImage()
	s.answer("ec2", "create-tags", "")
	if _, err := testAWS().BuildImage(context.Background(), testBuild(t, s)); err != nil {
		t.Fatal(err)
	}
	s.read("ops.args")
}

func TestBuildImageReportsTheFailureOfOps(t *testing.T) {
	s := newStage(t, true)
	write(t, filepath.Join(s.dir, "ops.fail"), "failed creating image: AccessDenied", 0o644)
	s.answer("ec2", "describe-images", "[]")
	_, err := testAWS().BuildImage(context.Background(), testBuild(t, s))
	if err == nil {
		t.Fatal("the build did not fail")
	}
	for _, part := range []string{"ops image create clanker-prod-v0.3.0", "AccessDenied",
		"clanker_env=prod"} {
		if !strings.Contains(err.Error(), part) {
			t.Fatalf("error = %v, want %q in it", err, part)
		}
	}
	if calls := s.awsCalls(); !slices.Equal(calls, []string{lookForBuiltImage}) {
		t.Fatalf("aws calls = %q, want only the look-up before ops", calls)
	}
}

func TestBuildImageFailsWhenOpsLeftNoImage(t *testing.T) {
	s := newStage(t, true)
	// ops 0.1.47 ends with the status 0 after some errors.
	write(t, filepath.Join(s.dir, "ops.says"), "Error: failed converting configuration file",
		0o644)
	s.answer("ec2", "describe-images", "[]")
	_, err := testAWS().BuildImage(context.Background(), testBuild(t, s))
	if err == nil {
		t.Fatal("the build did not fail")
	}
	for _, part := range []string{"0 images in us-west-2 have the tag Name=clanker-prod-v0.3.0",
		"failed converting configuration file"} {
		if !strings.Contains(err.Error(), part) {
			t.Fatalf("error = %v, want %q in it", err, part)
		}
	}
}

func TestBuildImageRefusesAReleaseWhoseImageExists(t *testing.T) {
	s := newStage(t, true)
	s.answer("ec2", "describe-images", builtImage)
	_, err := testAWS().BuildImage(context.Background(), testBuild(t, s))
	if err == nil || !strings.Contains(err.Error(), "clanker-prod-v0.3.0 exists already as "+
		testImageID) {
		t.Fatalf("error = %v, want a refusal of the existing image", err)
	}
	if _, err := os.Stat(filepath.Join(s.dir, "ops.args")); err == nil {
		t.Fatal("ops ran")
	}
}

func TestBuildImageReturnsTheImageWhenTheTagFails(t *testing.T) {
	s := newStage(t, true)
	s.opsMakesTheImage()
	s.refuse("ec2", "create-tags", "An error occurred (UnauthorizedOperation)")
	image, err := testAWS().BuildImage(context.Background(), testBuild(t, s))
	if err == nil || !strings.Contains(err.Error(), "UnauthorizedOperation") ||
		!strings.Contains(err.Error(), "tag the image "+testImageID) {
		t.Fatalf("error = %v, want the refused tag", err)
	}
	if image.ID != testImageID || image.SnapshotID != testSnapshotID {
		t.Fatalf("image = %+v, want the image that ops made, for the record", image)
	}
}

func recorded() Image {
	return Image{Name: "clanker-prod-v0.3.0", ID: testImageID, SnapshotID: testSnapshotID}
}

const (
	lookForImage = "ec2 describe-images --owners self --filters Name=image-id,Values=" +
		testImageID + " --query Images[].{name:Tags[?Key=='Name']|[0].Value}"
	deregister      = "ec2 deregister-image --image-id " + testImageID
	lookForSnapshot = "ec2 describe-snapshots --owner-ids self --filters " +
		"Name=snapshot-id,Values=" + testSnapshotID + " --query Snapshots[].SnapshotId"
	deleteSnapshot = "ec2 delete-snapshot --snapshot-id " + testSnapshotID
)

func TestRemoveImageDeregistersTheImageAndThenDeletesItsSnapshot(t *testing.T) {
	s := newStage(t, false)
	s.answer("ec2", "describe-images", `[{"name":"clanker-prod-v0.3.0"}]`)
	s.answer("ec2", "deregister-image", "")
	s.answer("ec2", "describe-snapshots", `["`+testSnapshotID+`"]`)
	s.answer("ec2", "delete-snapshot", "")
	if err := testAWS().RemoveImage(context.Background(), recorded()); err != nil {
		t.Fatal(err)
	}
	want := []string{lookForImage, deregister, lookForSnapshot, deleteSnapshot}
	if calls := s.awsCalls(); !slices.Equal(calls, want) {
		t.Fatalf("aws calls:\n%s\nwant:\n%s", strings.Join(calls, "\n"), strings.Join(want, "\n"))
	}
}

func TestRemoveImageFinishesARemovalThatStoppedHalfway(t *testing.T) {
	s := newStage(t, false)
	s.answer("ec2", "describe-images", "[]")
	s.answer("ec2", "describe-snapshots", `["`+testSnapshotID+`"]`)
	s.answer("ec2", "delete-snapshot", "")
	if err := testAWS().RemoveImage(context.Background(), recorded()); err != nil {
		t.Fatal(err)
	}
	want := []string{lookForImage, lookForSnapshot, deleteSnapshot}
	if calls := s.awsCalls(); !slices.Equal(calls, want) {
		t.Fatalf("aws calls = %q, want %q", calls, want)
	}
}

func TestRemoveImageWithNothingLeftChangesNothing(t *testing.T) {
	s := newStage(t, false)
	s.answer("ec2", "describe-images", "[]")
	s.answer("ec2", "describe-snapshots", "[]")
	if err := testAWS().RemoveImage(context.Background(), recorded()); err != nil {
		t.Fatal(err)
	}
	want := []string{lookForImage, lookForSnapshot}
	if calls := s.awsCalls(); !slices.Equal(calls, want) {
		t.Fatalf("aws calls = %q, want %q", calls, want)
	}
}

func TestRemoveImageRefusesAnImageWithAnotherName(t *testing.T) {
	s := newStage(t, false)
	s.answer("ec2", "describe-images", `[{"name":"someone-else"}]`)
	err := testAWS().RemoveImage(context.Background(), recorded())
	if err == nil || !strings.Contains(err.Error(), "nothing was removed") {
		t.Fatalf("error = %v, want a refusal", err)
	}
	if calls := s.awsCalls(); !slices.Equal(calls, []string{lookForImage}) {
		t.Fatalf("aws calls = %q, want only the look-up", calls)
	}
}

func TestRemoveImageRefusesARecordThatIsNotValid(t *testing.T) {
	s := newStage(t, false)
	image := recorded()
	image.SnapshotID = "snap-1; rm"
	err := testAWS().RemoveImage(context.Background(), image)
	if err == nil || !strings.Contains(err.Error(), "record of the image is not valid") {
		t.Fatalf("error = %v, want a refusal of the record", err)
	}
	if calls := s.awsCalls(); len(calls) != 0 {
		t.Fatalf("aws ran: %q", calls)
	}
}

func TestRemoveImageReportsAFailedCallWithItsContext(t *testing.T) {
	s := newStage(t, false)
	s.answer("ec2", "describe-images", `[{"name":"clanker-prod-v0.3.0"}]`)
	s.answer("ec2", "deregister-image", "")
	s.answer("ec2", "describe-snapshots", `["`+testSnapshotID+`"]`)
	s.refuse("ec2", "delete-snapshot", "An error occurred (InvalidSnapshot.InUse)")
	err := testAWS().RemoveImage(context.Background(), recorded())
	if err == nil || !strings.Contains(err.Error(), "delete the snapshot "+testSnapshotID) ||
		!strings.Contains(err.Error(), "InvalidSnapshot.InUse") {
		t.Fatalf("error = %v, want the failed deletion with its snapshot", err)
	}
}
