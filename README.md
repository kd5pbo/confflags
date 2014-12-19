NOT PRODUCTION.  USE AT YOUR OWN RISK (and submit pull requests)
=====================================

Config File / Flags Library
===========================

A slimmed down fork of https://github.com/vharitonsky/iniflags that returns
errors instead of printing to stderr and terminating your program.

Combines standard go flags with good old-fashioned config files.

Usage:

```bash

go get -u -a github.com/kd5pbo/confflags
```

main.go
```go
package main

import (
        "flag"
        ...
        "github.com/kd5pbo/confflags"
        ...
)

var (
        flag1 = flag.String("flag1", "default1", "Description1")
        flag2 = flag.Bool("flag2", false, "Description2")
        flag3 = flag.String("flag3", "default3", "Description3")
        flag4 = flgag.Bool("flag4", false, "Description4")
        flagN = flag.Int("flagN", 123, "DescriptionN")
)

func main() {
        confflags.Parse()  // use instead of flag.Parse()
        fmt.Printf("1: %v\n2: %v\n3: %v\n...\n4: %v\nN: %v\n",
                *flag1, *flag2, *flag3, *flag4, *flagN)
}
```

mydaemon.conf
```ini
# This is a comment line.
# Set flag1 to val1.
flag1 val1
# This line sets a boolean value to true.
# Functionally equivalent to "flag4 true"
flag4
flagN 4
```

Invocation
```bash
go run main.go -config mydaemon.conf -flag3=foobar
```

Output
```
1: val1
2: false
3: foobar
4: true
...
N: 4
```

Now all unset flags obtain their value from the file given with -config.
If the value is not found in the the config file, the flag will retain its
default value.

Flag value priority:
  - value set via command-line
  - value from config file
  - default value

Blank lines and lines starting with `#` are ignored.  Lines with only one word
(which must be the name of a flag), are treated as if " true" were also in the
line.  This is useful for boolean flags.  

All defined flags can be printed to stdout by passing -dumpflags on the
command line or specifying `dumpflags true` in the config file.  `Parse()`
will return `confflags.DumpedFlags` if so.  The following creates a config
file from the flags on the command line:

```bash
/path/to/the/program -flag1=val1 -flag3=foobar -flagN=4 -dumpflags > program.conf
```


Confflags also supports reloading the config file during runtime in two ways:

  * Via SIGHUP signal:

```bash
kill -s SIGHUP <program_pid>
```

  * Via the -configUpdateInterval flag. The following line will re-read config
every 3 minutes:

```bash
/path/to/the/program -config=/path/to/program.conf -configUpdateInterval=3m
```

This is useful for code such as
```go
package main

import (
        "flag"
        "github.com/kd5pbo/confflags"
        "log"
)

var listenPort = flag.Int("listenPort", 1234, "Port on which to listen.")
var s *server

func main() {
        iniflags.OnFlagChange("listenPort", func() {
                log.Printf("Server %v", confflags.Generation)
                n := startServerOnPort(*listenPort)
                s.Stop()
                s = n
        })
        /* confflags.Parse() starts the server on the -listenPort via the
        OnFlagChange() callback registered above. */
        uc := make(chan confflags.UpdateResult)
        iniflags.Parse(uc)
        /* Watch for updates */
        for u := range uc {
                if nil != u.Err {
                        log.Printf("Error updating config: %v", u.Err)
                } else {
                        log.Printf("Changed Flags: %V", u.ChangedFlags)
                }
}
```
If this is done, the function(s) registered with OnFlagChange will be called
once on startup.
