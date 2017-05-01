package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/dwarvesf/gitlab/utils"

	"github.com/Sirupsen/logrus"
	gitlab "github.com/xanzy/go-gitlab"
	kingpin "gopkg.in/alecthomas/kingpin.v2"
)

var (
	git *gitlab.Client
	// configFile = kingpin.Flag("config-file", "Path to config file").Short('c').Default("").String()
	action             = kingpin.Flag("action", "action to deploy").Short('a').Default("").String()
	projectID          = kingpin.Flag("project-id", "Project ID").Int()
	baseURL            = kingpin.Flag("base-url", "Gitlab base URL API").Short('b').String()
	userRole           = kingpin.Flag("user-role", "User's role").Short('u').Default("root").String()
	marathonFile       = kingpin.Flag("marathon-file", "Marathon template file").Short('m').Default("./marathon/marathon.json").String()
	marathonTargetFile = kingpin.Flag("marathon-target-file", "Marathon target file to deploy").Short('t').Default("./target.json").String()
	ignoreBuildVars    = kingpin.Flag("ignore-build-vars", "Build vars which be ignored").Short('v').
				Default("MARATHON_HOST", "CI_BUILD_DOCKER_HUB_PASSWORD", "CI_BUILD_DOCKER_HUB_USERNAME", "CONN").
				Strings()
	image = kingpin.Flag("image", "Docker image name to build").Short('i').String()
)

func main() {
	kingpin.Parse()

	if *baseURL == "" {
		logrus.Fatal("baseURL cannot be empty")
	}

	var err error
	// init git client
	git = gitlab.NewClient(nil, os.Getenv("GITLAB_PRIVATE_TOKEN"))
	git.SetBaseURL(*baseURL)
	optFunc := gitlab.WithSudo(*userRole)

	vars, err := utils.GetBuildVars(git, *projectID, nil, optFunc)
	if err != nil {
		logrus.WithError(err).Fatal("Cannot get build vars")
	}

	switch *action {
	case "gen-env-file":
		err = genEnvFile(vars)
	case "build-web":
		if *image == "" {
			logrus.Fatal("option image cannot be empty")
		}
		err = buildWeb(vars)
	case "gen-marathon-file":
		if _, err := os.Stat(*marathonFile); os.IsNotExist(err) {
			logrus.Fatalf("%v is not exist", *marathonFile)
		}
		err = genMarathonFile(vars)
	}

	if err != nil {
		logrus.Fatal(err)
	}
}

func genEnvFile(vars []*gitlab.BuildVariable) (err error) {
	var envs string
	for _, v := range vars {
		if utils.IsInSliceString(*ignoreBuildVars, v.Key) {
			continue
		}
		envs += fmt.Sprintf("%v=%v\n", v.Key, v.Value)
		os.Setenv(v.Key, v.Value)
	}

	err = utils.WriteFile("./env.env", envs)
	return
}

func buildWeb(vars []*gitlab.BuildVariable) (err error) {
	var args []string
	for _, v := range vars {
		if utils.IsInSliceString(*ignoreBuildVars, v.Key) {
			continue
		}
		os.Setenv(v.Key, v.Value)
		args = append(args, fmt.Sprintf("--build-arg %v=$%v", v.Key, v.Key))
	}

	cmd := fmt.Sprintf("docker build %v -t %v .", strings.Join(args, " "), *image)
	scriptPath := "./script.sh"
	err = utils.WriteFile(scriptPath, cmd)
	if err != nil {
		return
	}

	// run sh file to gen target.json
	logrus.Infof("Building image %v ...", *image)
	command := exec.Command("/bin/sh", scriptPath)
	err = command.Start()
	if err != nil {
		logrus.WithError(err).Error("Cannot start cmd")
		return
	}

	err = command.Wait()
	if err != nil {
		logrus.WithError(err).Error("Cannot wait cmd")
	}
	return
}

func genMarathonFile(vars []*gitlab.BuildVariable) (err error) {
	// create argument and env to create target.json
	logrus.Infof("Create arguments and envs to create %v ...", *marathonTargetFile)
	var args, envstr string
	var envs []string

	// args += "--arg build_id $CI_JOB_ID "
	// envs = append(envs, ".env.BUILD_ID |= $build_id")
	// os.Setenv("CI_JOB_ID", time.Now().String())
	for _, v := range vars {
		if utils.IsInSliceString(*ignoreBuildVars, v.Key) {
			continue
		}
		args += fmt.Sprintf("--arg %v $%v ", strings.ToLower(v.Key), v.Key)
		envs = append(envs, fmt.Sprintf(".env.%v |= $%v", v.Key, strings.ToLower(v.Key)))
		os.Setenv(v.Key, v.Value)
	}
	envstr = strings.Join(envs, " | ")

	// create sh flle to run command create target.json w marathon format
	cmd := fmt.Sprintf("cat %v | jq %v '%v' > %v", *marathonFile, args, envstr, *marathonTargetFile)
	scriptPath := "./script.sh"
	err = utils.WriteFile(scriptPath, cmd)
	if err != nil {
		return
	}

	// run sh file to gen target.json
	logrus.Infof("Running %v ...", scriptPath)
	command := exec.Command("/bin/sh", scriptPath)
	err = command.Start()
	return
}
