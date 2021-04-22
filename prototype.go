package prototype

import (
	prototype "github.com/aoldershaw/prototype-sdk-go"
)

func Prototype() prototype.Prototype {
	return prototype.New(
		prototype.WithIcon("mdi:oci"),
		prototype.WithObject(OCIImage{},
			prototype.WithMessage("build", RunBuild, BuildConfig),
		),
	)
}
