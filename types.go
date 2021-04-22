package prototype

// OCIImage is the object being acted upon by the prototype.
type OCIImage struct {
	Debug bool `json:"debug"`

	ContextDir     string            `json:"context" prototype:"required"`
	ContextInputs  map[string]string `json:"context_inputs,omitempty"`
	DockerfilePath string            `json:"dockerfile,omitempty"`

	Output string `json:"output" prototype:"required"`
	Cache  bool   `json:"cache,omitempty"`

	Target            string   `json:"target"`
	AdditionalTargets []string `json:"additional_targets"`

	BuildArgs []string `json:"build_args"`

	RegistryMirrors []string `json:"registry_mirrors"`

	Labels []string `json:"labels"`

	BuildkitSecrets map[string]string `json:"buildkit_secrets"`

	// Unpack the OCI image into Concourse's rootfs/ + metadata.json image scheme.
	//
	// Theoretically this would go away if/when we standardize on OCI.
	UnpackRootfs bool `json:"unpack_rootfs"`

	// Images to pre-load in order to avoid fetching at build time. Mapping from
	// build arg name to OCI image tarball path.
	//
	// Each image will be pre-loaded and a build arg will be set to a value
	// appropriate for setting in 'FROM ...'.
	ImageArgs []string `json:"image_args"`

	AddHosts string `json:"add_hosts"`
}

// ImageMetadata is the schema written to manifest.json when producing the
// legacy Concourse image format (rootfs/..., metadata.json).
type ImageMetadata struct {
	Env  []string `json:"env"`
	User string   `json:"user"`
}
