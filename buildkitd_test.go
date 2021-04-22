package prototype_test

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	prototype "github.com/aoldershaw/oci-image-prototype"
)

type BuildkitdSuite struct {
	suite.Suite
	*require.Assertions

	buildkitd  *prototype.Buildkitd
	outputsDir string
	ociImage   prototype.OCIImage
}

func (s *BuildkitdSuite) TearDownSuite() {
	if s.buildkitd != nil {
		err := s.buildkitd.Cleanup()
		s.NoError(err)
	}
}

func (s *BuildkitdSuite) SetupTest() {
	var err error
	s.outputsDir, err = ioutil.TempDir("", "oci-build-task-test")
	s.NoError(err)
}

func (s *BuildkitdSuite) TearDownTest() {
	err := os.RemoveAll(s.outputsDir)
	s.NoError(err)
}

func (s *BuildkitdSuite) TestNoConfig() {
	var pathExists bool

	if _, err := os.Stat(s.configPath()); err == nil {
		pathExists = true
	}

	s.Assert().False(pathExists)
}

func (s *BuildkitdSuite) TestGenerateConfig() {
	var err error

	s.ociImage.RegistryMirrors = []string{"hub.docker.io"}

	s.buildkitd, err = prototype.SpawnBuildkitd(s.ociImage, &prototype.BuildkitdOpts{
		ConfigPath: s.configPath("mirrors.toml"),
	})
	s.NoError(err)

	configContent, err := ioutil.ReadFile(s.configPath("mirrors.toml"))
	s.NoError(err)

	expectedContent, err := ioutil.ReadFile("testdata/buildkitd-config/mirrors.toml")
	s.NoError(err)

	s.Equal(expectedContent, configContent)
}

func (s *BuildkitdSuite) configPath(path ...string) string {
	return filepath.Join(append([]string{s.outputsDir, "config"}, path...)...)
}

func TestBuildkitd(t *testing.T) {
	suite.Run(t, &BuildkitdSuite{
		Assertions: require.New(t),
	})
}
