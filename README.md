godev
=====

A program to build, run, and restart a Go program on code change.
It also supports watching all your Go imports too. So if you
change the code of a library, your app will recompile.

This is not thoroughly tested. Report any issues you have.

#### TODO

Would be neat to incorporate https://github.com/daviddengcn/go-diff and only recompile when the Go code semantically changes.