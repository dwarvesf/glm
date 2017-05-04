package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/dwarvesf/glm/utils"

	"time"

	"github.com/Sirupsen/logrus"
	gitlab "github.com/xanzy/go-gitlab"
	kingpin "gopkg.in/alecthomas/kingpin.v2"
)

var (
	git                *gitlab.Client
	action             = kingpin.Flag("action", "action to deploy: gen-env-file, gen-marathon-file, build-web (beta)").Short('a').Default("").String()
	projectID          = kingpin.Flag("project-id", "Project ID").Int()
	baseURL            = kingpin.Flag("base-url", "Gitlab base URL API").Short('b').String()
	userRole           = kingpin.Flag("user-role", "User's role").Short('u').Default("root").String()
	marathonFile       = kingpin.Flag("marathon-file", "Marathon template file").Short('m').Default("./marathon/marathon.json").String()
	marathonTargetFile = kingpin.Flag("marathon-target-file", "Marathon target file to deploy").Short('t').Default("./target.json").String()
	ignoreBuildVars    = kingpin.Flag("ignore-build-vars", "Build vars which be ignored").Short('v').
				Default("CONN").
				Strings()
	additionalBuildVars = kingpin.Flag("additional-build-vars", "Additional Build vars which be added to build image").Short('d').
				Default("CI_JOB_ID").
				Strings()
	image                  = kingpin.Flag("image", "Docker image name to build").Short('i').String()
	gitlabPrivToken        = kingpin.Flag("gitlab-private-token", "Gitlab private token to get repo's information from Gitlab").Short('p').String()
	delayTime              = kingpin.Flag("delay-time", "Delay time (second) before run script.sh").Short('l').Int()
	defaultIgnoreBuildVars = []string{"MARATHON_HOST", "CI_BUILD_DOCKER_HUB_PASSWORD", "CI_BUILD_DOCKER_HUB_USERNAME", "GITLAB_PRIVATE_TOKEN"}
)

func main() {
	kingpin.Parse()

	if *baseURL == "" {
		logrus.Fatal("baseURL cannot be empty")
	}

	if *gitlabPrivToken == "" {
		logrus.Fatal("Gitlab private token cannot be empty")
	}

	var err error
	// init git client
	git = gitlab.NewClient(nil, *gitlabPrivToken)
	git.SetBaseURL(*baseURL)
	optFunc := gitlab.WithSudo(*userRole)

	// get vars from gitlab
	listBuildVarsOpt := &gitlab.ListBuildVariablesOptions{}
	vars, err := utils.GetBuildVars(git, *projectID, listBuildVarsOpt, optFunc)
	if err != nil {
		logrus.WithError(err).Fatal("Cannot get build vars")
	}

	// remove ignoredBuildVars when get vars from gitlab
	*ignoreBuildVars = append(*ignoreBuildVars, defaultIgnoreBuildVars...)
	vars = utils.RemoveListIgnoredBuildVars(vars, *ignoreBuildVars)

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
		envs += fmt.Sprintf("%v=%v\n", v.Key, v.Value)
	}

	return utils.WriteFile("./env.env", envs)
}

func buildWeb(vars []*gitlab.BuildVariable) (err error) {
	var args []string
	for _, v := range vars {
		args = append(args, fmt.Sprintf("--build-arg %v=$%v", v.Key, v.Key))
	}

	buildCmd := fmt.Sprintf("docker build %v -t %v .", strings.Join(args, " "), *image)
	logrus.Info(buildCmd)
	cmd := fmt.Sprintf("#!/bin/bash\nset -x\n%v", buildCmd)
	scriptPath := "./script.sh"
	err = utils.WriteFile(scriptPath, cmd)
	if err != nil {
		return
	}

	time.Sleep(time.Second * time.Duration(*delayTime))

	// run sh file to gen target.json
	logrus.Infof("Building image %v ...", *image)
	command := exec.Command("/bin/sh", scriptPath)
	cmdReader, err := command.StdoutPipe()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error creating StdoutPipe for Cmd", err)
		os.Exit(1)
	}

	scanner := bufio.NewScanner(cmdReader)
	go func() {
		for scanner.Scan() {
			fmt.Printf("%s\n", scanner.Text())
		}
	}()

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

	for _, v := range vars {
		args += fmt.Sprintf("--arg %v $%v ", strings.ToLower(v.Key), v.Key)
		envs = append(envs, fmt.Sprintf(".env.%v |= $%v", v.Key, strings.ToLower(v.Key)))
	}
	for _, v := range *additionalBuildVars {
		args += fmt.Sprintf("--arg %v $%v ", strings.ToLower(v), v)
		envs = append(envs, fmt.Sprintf(".env.%v |= $%v", v, strings.ToLower(v)))
	}
	envstr = strings.Join(envs, " | ")

	// create sh flle to run command create target.json w marathon format
	cmd := fmt.Sprintf("cat %v | jq %v '%v' > %v", *marathonFile, args, envstr, *marathonTargetFile)
	scriptPath := "./script.sh"
	err = utils.WriteFile(scriptPath, cmd)
	if err != nil {
		return
	}

	time.Sleep(time.Second * 1)

	// run sh file to gen target.json
	logrus.Infof("Running %v ...", scriptPath)
	command := exec.Command("/bin/sh", scriptPath)
	cmdReader, err := command.StdoutPipe()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error creating StdoutPipe for Cmd", err)
		os.Exit(1)
	}

	scanner := bufio.NewScanner(cmdReader)
	go func() {
		for scanner.Scan() {
			fmt.Printf("%s\n", scanner.Text())
		}
	}()

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
