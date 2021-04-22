package prototype

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	prototype "github.com/aoldershaw/prototype-sdk-go"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/u-root/u-root/pkg/termios"
)

func BuildConfig(img OCIImage) prototype.Config {
	var config prototype.Config

	config.Inputs = append(config.Inputs, prototype.Input{Name: img.ContextDir})
	for name, path := range img.ContextInputs {
		config.Inputs = append(config.Inputs, prototype.Input{
			Name: name,
			Path: filepath.Join(img.ContextDir, path),
		})
	}

	config.Outputs = []prototype.Output{{Name: img.Output, Path: "image"}}

	if img.Cache {
		config.Caches = []prototype.Cache{{Path: "cache"}}
	}

	return config
}

func RunBuild(img OCIImage) ([]prototype.MessageResponse, error) {
	wd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("get root path: %w", err)
	}

	// limit max columns; Concourse sets a super high value and buildctl happily
	// fills the whole screen with whitespace
	ws, err := termios.GetWinSize(os.Stdout.Fd())
	if err == nil {
		ws.Col = 100

		err = termios.SetWinSize(os.Stdout.Fd(), ws)
		if err != nil {
			logrus.Warn("failed to set window size:", err)
		}
	}

	var opts BuildkitdOpts
	if _, err := os.Stat("/scratch"); err == nil {
		opts.RootDir = "/scratch/buildkitd"
	}

	buildkitd, err := SpawnBuildkitd(img, &opts)
	if err != nil {
		return nil, fmt.Errorf("start buildkitd: %w", err)
	}

	err = Build(img, buildkitd, wd)
	if err != nil {
		return nil, fmt.Errorf("build: %w", err)
	}

	err = buildkitd.Cleanup()
	if err != nil {
		return nil, fmt.Errorf("cleanup buildkitd: %w", err)
	}

	return nil, nil
}

func Build(img OCIImage, buildkitd *Buildkitd, outputsDir string) error {
	if img.Debug {
		logrus.SetLevel(logrus.DebugLevel)
	}

	err := sanitize(&img)
	if err != nil {
		return errors.Wrap(err, "config")
	}

	cacheDir := filepath.Join(outputsDir, "cache")

	dockerfileDir := filepath.Dir(img.DockerfilePath)
	dockerfileName := filepath.Base(img.DockerfilePath)

	buildctlArgs := []string{
		"build",
		"--progress", "plain",
		"--frontend", "dockerfile.v0",
		"--local", "context=" + img.ContextDir,
		"--local", "dockerfile=" + dockerfileDir,
		"--opt", "filename=" + dockerfileName,
	}

	for _, arg := range img.Labels {
		buildctlArgs = append(buildctlArgs,
			"--opt", "label:"+arg,
		)
	}

	for _, arg := range img.BuildArgs {
		buildctlArgs = append(buildctlArgs,
			"--opt", "build-arg:"+arg,
		)
	}

	if len(img.ImageArgs) > 0 {
		imagePaths := map[string]string{}
		for _, arg := range img.ImageArgs {
			segs := strings.SplitN(arg, "=", 2)
			imagePaths[segs[0]] = segs[1]
		}

		registry, err := LoadRegistry(imagePaths)
		if err != nil {
			return fmt.Errorf("create local image registry: %w", err)
		}

		port, err := ServeRegistry(registry)
		if err != nil {
			return fmt.Errorf("serve local image registry: %w", err)
		}

		for _, arg := range registry.BuildArgs(port) {
			buildctlArgs = append(buildctlArgs,
				"--opt", "build-arg:"+arg,
			)
		}
	}

	if _, err := os.Stat(cacheDir); err == nil {
		buildctlArgs = append(buildctlArgs,
			"--export-cache", "type=local,mode=min,dest="+cacheDir,
		)
	}

	for id, src := range img.BuildkitSecrets {
		buildctlArgs = append(buildctlArgs,
			"--secret", "id="+id+",src="+src,
		)
	}

	var builds [][]string
	var targets []string
	var imagePaths []string

	for _, t := range img.AdditionalTargets {
		// prevent re-use of the buildctlArgs slice as it is appended to later on,
		// and that would clobber args for all targets if the slice was re-used
		targetArgs := make([]string, len(buildctlArgs))
		copy(targetArgs, buildctlArgs)

		targetArgs = append(targetArgs, "--opt", "target="+t)

		targetDir := filepath.Join(outputsDir, t)

		if _, err := os.Stat(targetDir); err == nil {
			imagePath := filepath.Join(targetDir, "image.tar")
			imagePaths = append(imagePaths, imagePath)

			targetArgs = append(targetArgs,
				"--output", "type=docker,dest="+imagePath,
			)
		}

		builds = append(builds, targetArgs)
		targets = append(targets, t)
	}

	finalTargetDir := filepath.Join(outputsDir, "image")
	if _, err := os.Stat(finalTargetDir); err == nil {
		imagePath := filepath.Join(finalTargetDir, "image.tar")
		imagePaths = append(imagePaths, imagePath)

		buildctlArgs = append(buildctlArgs,
			"--output", "type=docker,dest="+imagePath,
		)
	}

	if img.Target != "" {
		buildctlArgs = append(buildctlArgs,
			"--opt", "target="+img.Target,
		)
	}

	if img.AddHosts != "" {
		buildctlArgs = append(buildctlArgs,
			"--opt", "add-hosts="+img.AddHosts,
		)
	}

	builds = append(builds, buildctlArgs)
	targets = append(targets, "")

	for i, args := range builds {
		if i > 0 {
			fmt.Fprintln(os.Stderr)
		}

		targetName := targets[i]
		if targetName == "" {
			logrus.Info("building image")
		} else {
			logrus.Infof("building target '%s'", targetName)
		}

		if _, err := os.Stat(filepath.Join(cacheDir, "index.json")); err == nil {
			args = append(args,
				"--import-cache", "type=local,src="+cacheDir,
			)
		}

		logrus.Debugf("running buildctl %s", strings.Join(args, " "))

		err = buildctl(buildkitd.Addr, os.Stdout, args...)
		if err != nil {
			return errors.Wrap(err, "build")
		}
	}

	for _, imagePath := range imagePaths {
		image, err := tarball.ImageFromPath(imagePath, nil)
		if err != nil {
			return errors.Wrap(err, "open oci image")
		}

		outputDir := filepath.Dir(imagePath)

		err = writeDigest(outputDir, image)
		if err != nil {
			return err
		}

		if img.UnpackRootfs {
			err = unpackRootfs(outputDir, image, img)
			if err != nil {
				return errors.Wrap(err, "unpack rootfs")
			}
		}
	}

	return nil
}

func writeDigest(dest string, image v1.Image) error {
	digestPath := filepath.Join(dest, "digest")

	manifest, err := image.Manifest()
	if err != nil {
		return errors.Wrap(err, "get image digest")
	}

	err = ioutil.WriteFile(digestPath, []byte(manifest.Config.Digest.String()), 0644)
	if err != nil {
		return errors.Wrap(err, "write digest file")
	}

	return nil
}

func unpackRootfs(dest string, image v1.Image, img OCIImage) error {
	rootfsDir := filepath.Join(dest, "rootfs")
	metadataPath := filepath.Join(dest, "metadata.json")

	logrus.Info("unpacking image")

	err := unpackImage(rootfsDir, image, img.Debug)
	if err != nil {
		return errors.Wrap(err, "unpack image")
	}

	err = writeImageMetadata(metadataPath, image)
	if err != nil {
		return errors.Wrap(err, "write image metadata")
	}

	return nil
}

func writeImageMetadata(metadataPath string, image v1.Image) error {
	cfg, err := image.ConfigFile()
	if err != nil {
		return errors.Wrap(err, "load image config")
	}

	meta, err := os.Create(metadataPath)
	if err != nil {
		return errors.Wrap(err, "create metadata file")
	}

	err = json.NewEncoder(meta).Encode(ImageMetadata{
		Env:  cfg.Config.Env,
		User: cfg.Config.User,
	})
	if err != nil {
		return errors.Wrap(err, "encode metadata")
	}

	err = meta.Close()
	if err != nil {
		return errors.Wrap(err, "close meta")
	}

	return nil
}

func sanitize(img *OCIImage) error {
	if img.ContextDir == "" {
		img.ContextDir = "."
	}

	if img.DockerfilePath == "" {
		img.DockerfilePath = filepath.Join(img.ContextDir, "Dockerfile")
	}

	return nil
}

func buildctl(addr string, out io.Writer, args ...string) error {
	return run(out, "buildctl", append([]string{"--addr=" + addr}, args...)...)
}

func run(out io.Writer, path string, args ...string) error {
	cmd := exec.Command(path, args...)
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.Stdin = os.Stdin
	return cmd.Run()
}
