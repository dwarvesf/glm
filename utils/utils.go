package utils

import (
	"os"

	"strconv"

	"github.com/Sirupsen/logrus"
	gitlab "github.com/xanzy/go-gitlab"
)

// IsInSliceString check if a string is in a slice or not
func IsInSliceString(list []string, s string) bool {
	for _, v := range list {
		if s == v {
			return true
		}
	}
	return false
}

// WriteFile create and write file w given content
func WriteFile(filePath, content string) (err error) {
	logrus.Infof("Creating %v ...", filePath)
	f, err := os.Create(filePath)
	defer f.Close()
	if err != nil {
		logrus.WithError(err).Error("Cannot create file")
		return
	}

	_, err = f.WriteString(content)
	if err != nil {
		logrus.WithError(err).Error("Cannot write to file")
		return
	}
	f.Sync()
	return
}

// GetBuildVars get build vars from gitlab's project w given projectID
func GetBuildVars(git *gitlab.Client, pid interface{}, opts *gitlab.ListBuildVariablesOptions, options gitlab.OptionFunc) (vars []*gitlab.BuildVariable, err error) {
	logrus.Info("Getting build vars from project ...")
	vars, resp, err := git.BuildVariables.ListBuildVariables(pid, opts, options)
	if err != nil {
		return
	}
	total, _ := strconv.Atoi(resp.Header.Get("X-Total"))
	if total > len(vars) {
		opts.PerPage = total
		vars, _, err = git.BuildVariables.ListBuildVariables(pid, opts, options)
	}
	return
}

// RemoveListIgnoredBuildVars remove unecessary build vars
func RemoveListIgnoredBuildVars(vars []*gitlab.BuildVariable, ignoredBuildVars []string) []*gitlab.BuildVariable {
	for i := range ignoredBuildVars {
		for j := range vars {
			if ignoredBuildVars[i] == vars[j].Key {
				vars = append(vars[:j], vars[j+1:]...)
				break
			}
		}
	}
	return vars
}
