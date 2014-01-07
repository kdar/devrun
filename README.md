devrun
======

A program to build, run, and restart a Go program on code change.
It also supports watching all your Go imports too. So if you
change the code of a library, your app will recompile.

This is not thoroughly tested. Report any issues you have.

#### Notes

It is important that in the --run option you use "exec" when you want to run a command that should be killed when godev detects code changes.

#### Examples

    devrun watch --files "^(.*\.go|.*\.yaml|.*\.conf)$" --run "godep go build && exec ./prog run"

    devrun watch --run "go test"

#### TODO

Would be neat to incorporate https://github.com/daviddengcn/go-diff and only recompile when the Go code semantically changes.