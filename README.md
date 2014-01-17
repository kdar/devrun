devrun
======

A program to build, run, and restart a Go program on code change.
It also supports watching all your Go imports too. So if you
change the code of a library, your app will recompile.

This is not thoroughly tested. Report any issues you have.

#### Notes

The watch subcommand will run whatever commands you pass in its own shell. What it does behind the scenes is run `sh -c "your commands here"`. For example, `devrun watch go test` will run `sh -c "go test"`. This means you can use any one liner shell script in here. This also means that if you have a long running process (the process doesn't exit in a short amount of time) such as a webserver or other service, you must use "exec" if you want devrun to be able to kill it and restart the process. 

#### Examples

    devrun watch --files "^(.*\.go|.*\.yaml|.*\.conf)$" "godep go build && exec ./prog run"

    devrun watch go test

    devrun watch -- go test -run="TestFunc"

    devrun watch exec ./webserver

#### TODO

Would be neat to incorporate https://github.com/daviddengcn/go-diff and only recompile when the Go code semantically changes.