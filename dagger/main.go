// A generated module for Site functions
//
// This module has been generated via dagger init and serves as a reference to
// basic module structure as you get started with Dagger.
//
// Two functions have been pre-created. You can modify, delete, or add to them,
// as needed. They demonstrate usage of arguments and return types using simple
// echo and grep commands. The functions can be called from the dagger CLI or
// from one of the SDKs.
//
// The first line in this comment block is a short description line and the
// rest is a long description with more detail on the module's purpose or usage,
// if appropriate. All modules should have a short description.

package main

import (
	"context"
	"dagger/site/internal/dagger"
	"fmt"
	"time"
)

type Site struct{}

const hugoVersion = "0.128.2"

func (s *Site) BuildEnv(source *dagger.Directory) *dagger.Container {
	return dag.Container().
		From(fmt.Sprintf("hugomods/hugo:exts-%s", hugoVersion)).
		WithDirectory("/src", source).
		WithWorkdir("/src")
}

func (s *Site) Build(source *dagger.Directory) *dagger.Container {
	built := s.BuildEnv(source).
		WithExec([]string{"hugo"}).
		Directory("/src/public")

	return dag.Container().
		From("cgr.dev/chainguard/nginx").
		WithDirectory("/usr/share/nginx/html", built).
		WithExposedPort(8080)
}

func (s *Site) BuildAndPublish(ctx context.Context,
	source *dagger.Directory,
	registry string,
	username string,
	// +optional
	ref string,
	password *dagger.Secret) (string, error) {
	if ref == "" {
		ref = "localdev"
	}
	return s.Build(source).
		WithRegistryAuth(registry, username, password).
		Publish(ctx, fmt.Sprintf("%s/images/website:%s-%s", registry, ref[0:8], time.Now().UTC().Format("20060102150405")))
}
