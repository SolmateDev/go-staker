package test

import (
	"context"
	"os"
	"os/exec"
	"time"
)

func BaseDir() (string, error) {
	return os.Getwd()
}

func Validator(ctx context.Context, baseDir string) Child {
	child := CreateCmd(ctx, exec.CommandContext(ctx, baseDir+"/contrib/test.sh", "validator"))

	time.Sleep(10 * time.Second)

	return child
}

func DeployPrograms(ctx context.Context, baseDir string) Child {
	return CreateCmd(ctx, exec.CommandContext(ctx, baseDir+"/contrib/test.sh", "deploy"))
}
