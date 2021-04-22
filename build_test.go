package prototype_test

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	prototype "github.com/aoldershaw/oci-image-prototype"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type TaskSuite struct {
	suite.Suite
	*require.Assertions

	buildkitd  *prototype.Buildkitd
	outputsDir string
	ociImage   prototype.OCIImage
}

func (s *TaskSuite) SetupSuite() {
	var err error
	s.buildkitd, err = prototype.SpawnBuildkitd(prototype.OCIImage{}, nil)
	s.NoError(err)
}

func (s *TaskSuite) TearDownSuite() {
	err := s.buildkitd.Cleanup()
	s.NoError(err)
}

func (s *TaskSuite) SetupTest() {
	var err error
	s.outputsDir, err = ioutil.TempDir("", "oci-image-prototype-test")
	s.NoError(err)

	err = os.Mkdir(s.imagePath(), 0755)
	s.NoError(err)

	s.ociImage = prototype.OCIImage{
		Debug: true,
	}
}

func (s *TaskSuite) TearDownTest() {
	err := os.RemoveAll(s.outputsDir)
	s.NoError(err)
}

func (s *TaskSuite) TestBasicBuild() {
	s.ociImage.ContextDir = "testdata/basic"

	err := s.build()
	s.NoError(err)
}

func (s *TaskSuite) TestNoOutputBuild() {
	s.ociImage.ContextDir = "testdata/basic"

	err := os.RemoveAll(s.imagePath())
	s.NoError(err)

	err = s.build()
	s.NoError(err)
}

func (s *TaskSuite) TestDigestFile() {
	s.ociImage.ContextDir = "testdata/basic"

	err := s.build()
	s.NoError(err)

	digest, err := ioutil.ReadFile(s.imagePath("digest"))
	s.NoError(err)

	image, err := tarball.ImageFromPath(s.imagePath("image.tar"), nil)
	s.NoError(err)

	manifest, err := image.Manifest()
	s.NoError(err)

	s.Equal(string(digest), manifest.Config.Digest.String())
}

func (s *TaskSuite) TestDockerfilePath() {
	s.ociImage.ContextDir = "testdata/dockerfile-path"
	s.ociImage.DockerfilePath = "testdata/dockerfile-path/hello.Dockerfile"

	err := s.build()
	s.NoError(err)
}

func (s *TaskSuite) TestTarget() {
	s.ociImage.ContextDir = "testdata/target"
	s.ociImage.Target = "working-target"

	err := s.build()
	s.NoError(err)
}

func (s *TaskSuite) TestBuildArgs() {
	s.ociImage.ContextDir = "testdata/build-args"
	s.ociImage.BuildArgs = []string{
		"some_arg=some_value",
		"some_other_arg=some_other_value",
	}

	// the Dockerfile itself asserts that the arg has been received
	err := s.build()
	s.NoError(err)
}

func (s *TaskSuite) TestLabels() {
	s.ociImage.ContextDir = "testdata/labels"
	expectedLabels := map[string]string{
		"some_label":       "some_value",
		"some_other_label": "some_other_value",
	}
	s.ociImage.Labels = make([]string, 0, len(expectedLabels))

	for k, v := range expectedLabels {
		s.ociImage.Labels = append(s.ociImage.Labels, fmt.Sprintf("%s=%s", k, v))
	}

	err := s.build()
	s.NoError(err)

	image, err := tarball.ImageFromPath(s.imagePath("image.tar"), nil)
	s.NoError(err)

	configFile, err := image.ConfigFile()
	s.NoError(err)

	s.True(reflect.DeepEqual(expectedLabels, configFile.Config.Labels))
}

func (s *TaskSuite) TestUnpackRootfs() {
	s.ociImage.ContextDir = "testdata/unpack-rootfs"
	s.ociImage.UnpackRootfs = true

	err := s.build()
	s.NoError(err)

	meta, err := s.imageMetadata("image")
	s.NoError(err)

	rootfsContent, err := ioutil.ReadFile(s.imagePath("rootfs", "Dockerfile"))
	s.NoError(err)

	expectedContent, err := ioutil.ReadFile("testdata/unpack-rootfs/Dockerfile")
	s.NoError(err)

	s.Equal(rootfsContent, expectedContent)

	s.Equal(meta.User, "banana")
	s.Equal(meta.Env, []string{"PATH=/darkness", "BA=nana"})
}

func (s *TaskSuite) TestBuildkitSecrets() {
	s.ociImage.ContextDir = "testdata/buildkit-secret"
	s.ociImage.BuildkitSecrets = map[string]string{"secret": "testdata/buildkit-secret/secret"}

	err := s.build()
	s.NoError(err)
}

func (s *TaskSuite) TestRegistryMirrors() {
	mirror := httptest.NewServer(registry.New())
	defer mirror.Close()

	image, err := random.Image(1024, 2)
	s.NoError(err)

	mirrorURL, err := url.Parse(mirror.URL)
	s.NoError(err)

	mirrorRef, err := name.NewTag(fmt.Sprintf("%s/library/mirrored-image:some-tag", mirrorURL.Host))
	s.NoError(err)

	err = remote.Write(mirrorRef, image)
	s.NoError(err)

	s.ociImage.ContextDir = "testdata/mirror"
	s.ociImage.RegistryMirrors = []string{mirrorURL.Host}

	rootDir, err := ioutil.TempDir("", "mirrored-buildkitd")
	s.NoError(err)

	defer os.RemoveAll(rootDir)

	mirroredBuildkitd, err := prototype.SpawnBuildkitd(s.ociImage, &prototype.BuildkitdOpts{
		RootDir: rootDir,
	})
	s.NoError(err)

	defer mirroredBuildkitd.Cleanup()

	err = prototype.Build(s.ociImage, mirroredBuildkitd, s.outputsDir)
	s.NoError(err)

	builtImage, err := tarball.ImageFromPath(s.imagePath("image.tar"), nil)
	s.NoError(err)

	layers, err := image.Layers()
	s.NoError(err)

	builtLayers, err := builtImage.Layers()
	s.NoError(err)
	s.Len(builtLayers, len(layers))

	for i := 0; i < len(layers); i++ {
		digest, err := layers[i].Digest()
		s.NoError(err)

		builtDigest, err := builtLayers[i].Digest()
		s.NoError(err)

		s.Equal(digest, builtDigest)
	}
}

func (s *TaskSuite) TestImageArgs() {
	imagesDir, err := ioutil.TempDir("", "preload-images")
	s.NoError(err)

	defer os.RemoveAll(imagesDir)

	firstImage, err := random.Image(1024, 2)
	s.NoError(err)
	firstPath := filepath.Join(imagesDir, "first.tar")
	err = tarball.WriteToFile(firstPath, nil, firstImage)
	s.NoError(err)

	secondImage, err := random.Image(1024, 2)
	s.NoError(err)
	secondPath := filepath.Join(imagesDir, "second.tar")
	err = tarball.WriteToFile(secondPath, nil, secondImage)
	s.NoError(err)

	s.ociImage.ContextDir = "testdata/image-args"
	s.ociImage.AdditionalTargets = []string{"first"}
	s.ociImage.ImageArgs = []string{
		"first_image=" + firstPath,
		"second_image=" + secondPath,
	}

	err = os.Mkdir(s.outputPath("first"), 0755)
	s.NoError(err)

	err = s.build()
	s.NoError(err)

	firstBuiltImage, err := tarball.ImageFromPath(s.outputPath("first", "image.tar"), nil)
	s.NoError(err)

	secondBuiltImage, err := tarball.ImageFromPath(s.outputPath("image", "image.tar"), nil)
	s.NoError(err)

	for image, builtImage := range map[v1.Image]v1.Image{
		firstImage:  firstBuiltImage,
		secondImage: secondBuiltImage,
	} {
		layers, err := image.Layers()
		s.NoError(err)

		builtLayers, err := builtImage.Layers()
		s.NoError(err)
		s.Len(builtLayers, len(layers)+1)

		for i := 0; i < len(layers); i++ {
			digest, err := layers[i].Digest()
			s.NoError(err)

			builtDigest, err := builtLayers[i].Digest()
			s.NoError(err)

			s.Equal(digest, builtDigest)
		}
	}
}

func (s *TaskSuite) TestImageArgsUnpack() {
	imagesDir, err := ioutil.TempDir("", "preload-images")
	s.NoError(err)

	defer os.RemoveAll(imagesDir)

	image, err := random.Image(1024, 2)
	s.NoError(err)
	imagePath := filepath.Join(imagesDir, "first.tar")
	err = tarball.WriteToFile(imagePath, nil, image)
	s.NoError(err)

	s.ociImage.ContextDir = "testdata/image-args"
	s.ociImage.AdditionalTargets = []string{"first"}
	s.ociImage.ImageArgs = []string{
		"first_image=" + imagePath,
		"second_image=" + imagePath,
	}
	s.ociImage.UnpackRootfs = true

	err = s.build()
	s.NoError(err)

	meta, err := s.imageMetadata("image")
	s.NoError(err)

	rootfsContent, err := ioutil.ReadFile(s.imagePath("rootfs", "Dockerfile.second"))
	s.NoError(err)

	expectedContent, err := ioutil.ReadFile("testdata/image-args/Dockerfile")
	s.NoError(err)

	s.Equal(rootfsContent, expectedContent)

	s.Equal(meta.User, "banana")
	s.Equal(meta.Env, []string{"PATH=/darkness", "BA=nana"})
}

func (s *TaskSuite) TestMultiTarget() {
	s.ociImage.ContextDir = "testdata/multi-target"
	s.ociImage.AdditionalTargets = []string{"additional-target"}

	err := os.Mkdir(s.outputPath("additional-target"), 0755)
	s.NoError(err)

	err = s.build()
	s.NoError(err)

	finalImage, err := tarball.ImageFromPath(s.imagePath("image.tar"), nil)
	s.NoError(err)

	finalCfg, err := finalImage.ConfigFile()
	s.NoError(err)
	s.Equal("final-target", finalCfg.Config.Labels["target"])

	additionalImage, err := tarball.ImageFromPath(s.outputPath("additional-target", "image.tar"), nil)
	s.NoError(err)

	additionalCfg, err := additionalImage.ConfigFile()
	s.NoError(err)
	s.Equal("additional-target", additionalCfg.Config.Labels["target"])
}

func (s *TaskSuite) TestMultiTargetExplicitTarget() {
	s.ociImage.ContextDir = "testdata/multi-target"
	s.ociImage.AdditionalTargets = []string{"additional-target"}
	s.ociImage.Target = "final-target"

	err := os.Mkdir(s.outputPath("additional-target"), 0755)
	s.NoError(err)

	err = s.build()
	s.NoError(err)

	finalImage, err := tarball.ImageFromPath(s.imagePath("image.tar"), nil)
	s.NoError(err)

	finalCfg, err := finalImage.ConfigFile()
	s.NoError(err)
	s.Equal("final-target", finalCfg.Config.Labels["target"])

	additionalImage, err := tarball.ImageFromPath(s.outputPath("additional-target", "image.tar"), nil)
	s.NoError(err)

	additionalCfg, err := additionalImage.ConfigFile()
	s.NoError(err)
	s.Equal("additional-target", additionalCfg.Config.Labels["target"])
}

func (s *TaskSuite) TestMultiTargetDigest() {
	s.ociImage.ContextDir = "testdata/multi-target"
	s.ociImage.AdditionalTargets = []string{"additional-target"}

	err := os.Mkdir(s.outputPath("additional-target"), 0755)
	s.NoError(err)

	err = s.build()
	s.NoError(err)

	additionalImage, err := tarball.ImageFromPath(s.outputPath("additional-target", "image.tar"), nil)
	s.NoError(err)
	digest, err := ioutil.ReadFile(s.outputPath("additional-target", "digest"))
	s.NoError(err)
	additionalManifest, err := additionalImage.Manifest()
	s.NoError(err)
	s.Equal(string(digest), additionalManifest.Config.Digest.String())

	finalImage, err := tarball.ImageFromPath(s.imagePath("image.tar"), nil)
	s.NoError(err)
	digest, err = ioutil.ReadFile(s.outputPath("image", "digest"))
	s.NoError(err)
	finalManifest, err := finalImage.Manifest()
	s.NoError(err)
	s.Equal(string(digest), finalManifest.Config.Digest.String())
}

func (s *TaskSuite) TestMultiTargetUnpack() {
	s.ociImage.ContextDir = "testdata/multi-target"
	s.ociImage.AdditionalTargets = []string{"additional-target"}
	s.ociImage.UnpackRootfs = true

	err := os.Mkdir(s.outputPath("additional-target"), 0755)
	s.NoError(err)

	err = s.build()
	s.NoError(err)

	rootfsContent, err := ioutil.ReadFile(s.outputPath("additional-target", "rootfs", "Dockerfile.banana"))
	s.NoError(err)
	expectedContent, err := ioutil.ReadFile("testdata/multi-target/Dockerfile")
	s.NoError(err)
	s.Equal(rootfsContent, expectedContent)

	meta, err := s.imageMetadata("additional-target")
	s.NoError(err)
	s.Equal(meta.User, "banana")
	s.Equal(meta.Env, []string{"PATH=/darkness", "BA=nana"})

	rootfsContent, err = ioutil.ReadFile(s.outputPath("image", "rootfs", "Dockerfile.orange"))
	s.NoError(err)
	expectedContent, err = ioutil.ReadFile("testdata/multi-target/Dockerfile")
	s.NoError(err)
	s.Equal(rootfsContent, expectedContent)

	meta, err = s.imageMetadata("image")
	s.NoError(err)
	s.Equal(meta.User, "orange")
	s.Equal(meta.Env, []string{"PATH=/lightness", "OR=ange"})
}

func (s *TaskSuite) TestAddHosts() {
	s.ociImage.ContextDir = "testdata/add-hosts"
	s.ociImage.AddHosts = "test-host=1.2.3.4"

	err := s.build()
	s.NoError(err)
}

func (s *TaskSuite) build() error {
	return prototype.Build(s.ociImage, s.buildkitd, s.outputsDir)
}

func (s *TaskSuite) imagePath(path ...string) string {
	return s.outputPath(append([]string{"image"}, path...)...)
}

func (s *TaskSuite) outputPath(path ...string) string {
	return filepath.Join(append([]string{s.outputsDir}, path...)...)
}

func (s *TaskSuite) imageMetadata(output string) (prototype.ImageMetadata, error) {
	metadataPayload, err := ioutil.ReadFile(s.outputPath(output, "metadata.json"))
	if err != nil {
		return prototype.ImageMetadata{}, err
	}

	var meta prototype.ImageMetadata
	err = json.Unmarshal(metadataPayload, &meta)
	if err != nil {
		return prototype.ImageMetadata{}, err
	}

	return meta, nil
}

func TestSuite(t *testing.T) {
	suite.Run(t, &TaskSuite{
		Assertions: require.New(t),
	})
}
