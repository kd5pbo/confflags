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
        ...
        flagN = flag.Int("flagN", 123, "DescriptionN")
)

func main() {
        confflags.Parse()  // use instead of flag.Parse()
}
```

mydaemon.conf

```ini
        # comment1
        flag1 val1
        
        ...
        
        flagN 4
```

```bash

go run main.go -config mydaemon.conf -flag3=foobar

```

Now all unset flags obtain their value from the file given with -config.
If the value is not found in the the config file, the flag will retain its
default value.

Flag value priority:
  - value set via command-line
  - value from config file
  - default value

Blank lines and lines starting with `#` are ignored.  All other lines must have
at least two parts, a key and a value separated by white space.

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


Advanced usage.

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
                n := startServerOnPort(*listenPort)
                s.Stop()
                s = n
        })
        /* confflags.Parse() starts the server on the -listenPort via the
        OnFlagChange() callback registered above. */
        iniflags.Parse()
}
```
