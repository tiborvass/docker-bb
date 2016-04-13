package main

import (
	"fmt"
	"io/ioutil"
	"os/exec"
	"path"
	"strings"
	"time"

	"github.com/Sirupsen/logrus"
)

// get the version for the binaries built
func getBinaryVersion(temp string) (version string, err error) {
	file, err := ioutil.ReadFile(path.Join(temp, "VERSION"))
	if err != nil {
		return version, err
	}

	return strings.TrimSpace(string(file)), nil
}

// checkout `git clones` a repo
func checkout(temp, repo, sha string) error {
	// don't clone the whole repo, it's too slow
	cmd := exec.Command("git", "clone", "--depth=100", "--recursive", "--branch=master", repo, temp)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("Running command failed: %s, %v", string(output), err)
	}

	// checkout a commit (or branch or tag) of interest
	cmd = exec.Command("git", "checkout", "-qf", sha)
	cmd.Dir = temp
	output, err = cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("Running command failed: %s, %v", string(output), err)
	}

	return nil
}

// build the docker image
func build(temp, name string) error {
	if out, err := exec.Command(strings.Fields(`sed -ri 's/^(ENV GO_VERSION) 1.5.4/\1 1.5.3/' Dockerfile`)).CombinedOutput(); err != nil {
		return fmt.Errorf("Could not change Go version in Dockerfile for %s: %s", name, out)
	}
	cmd := exec.Command("docker", "build", "-t", name, ".")
	cmd.Dir = temp

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("Running command failed: %s, %v", string(output), err)
	}
	return nil
}

// make the binaries
func makeBinary(temp, image, name string, duration time.Duration) error {
	var (
		c   = make(chan error)
		cmd = exec.Command("docker", "run", "-i", "--privileged", "-e", "DOCKER_CROSSPLATFORMS=windows/amd64", "--name", name, "-v", path.Join(temp, "bundles")+":/go/src/github.com/docker/docker/bundles", image, "hack/make.sh", "cross")
	)
	cmd.Dir = temp

	go func() {
		output, err := cmd.CombinedOutput()
		if err != nil {
			// it's ok for the make command to return a non-zero exit
			// incase of a failed build
			if _, ok := err.(*exec.ExitError); !ok {
				logrus.Infof("Build failed: %s", string(output))
			} else {
				if string(output) != "" {
					logrus.Debugf("Container log: %s", string(output))
				}
				err = nil
			}
		} else if string(output) != "" {
			logrus.Debugf("Container log: %s", string(output))
		}

		output, err = exec.Command("docker", "wait", name).CombinedOutput()
		if err != nil {
			logrus.Infof("Waiting failed: %s", string(output))
		}

		c <- err
	}()

	select {
	case err := <-c:
		if err != nil {
			return err
		}
	case <-time.After(duration):
		if err := cmd.Process.Kill(); err != nil {
			logrus.Infof("Killing process failed: %v", err)
		}
		return fmt.Errorf("Killed because build took to long")
	}
	return nil
}

// remove the build container
func removeContainer(container string) {
	cmd := exec.Command("docker", "rm", "-f", container)
	output, err := cmd.CombinedOutput()
	if err != nil {
		logrus.Warnf("Removing container failed: %s, %v", string(output), err)
	}
}
