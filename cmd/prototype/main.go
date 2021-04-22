package main

import (
	prototype "github.com/aoldershaw/oci-image-prototype"
	"github.com/sirupsen/logrus"
)

func main() {
	if err := prototype.Prototype().Run(); err != nil {
		logrus.Fatal(err)
	}
}
