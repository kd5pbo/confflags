// Package confflags provides an easy way to read configuration information
// from both the command line and a config file.
package confflags

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
)

/* Library-specific command line flags */
var (
	config = flag.String("config", "", "Path to ini config for using in "+
		"go flags. May be relative to the current executable path.")
	configUpdateInterval = flag.Duration("configUpdateInterval", 0,
		"Update interval for re-reading config file set via -config "+
			"flag. Zero disables config file re-reading.")
	dumpflags = flag.Bool("dumpflags", false, "Dumps values for all "+
		"flags defined in the app into stdout in ini-compatible "+
		"syntax and terminates the app.")
)

/* State variables */
var (
	flagChangeCallbacks = make(map[string][]FlagChangeCallback)
	importStack         []string
	parsed              bool
	updateLock          sync.Mutex /* Concurrent updates would be bad */
	/* Wake up the interval watcher */
	cond = sync.NewCond(&sync.Mutex{})
)

/* Regular expression to split lines */
var splitRE = regexp.MustCompile(`\s+`)

var (
	// flags' generation number.
	// It is modified on each flags' modification
	// via either -configUpdateInterval or SIGHUP.
	Generation = 0
	// DumpedFlags is the error returned when Parse() is called and
	// -dumpflags is given on the command line.
	DumpedFlags = errors.New("Dumped")
)

// Use instead of flag.Parse().  If c is not nil, results from updating the
// config file either via SIGHUP or -configUpdateInterval will be sent out on
// it.
func Parse(c chan UpdateResult) error {
	/* Don't double-parse */
	if parsed {
		return fmt.Errorf("flags already parsed")
	}

	/* Parse the flags on the command line */
	flag.Parse()
	parsed = true

	/* Get the key/value pairs from the config file */
	if _, err := parseConfigFlags(); nil != err {
		return err
	}

	/* Print the current state, if requested */
	if *dumpflags {
		dumpFlags()
		return DumpedFlags
	}

	/* Now that we have all the flags, make sure there's no extra
	callbacks registered */
	for flagName, _ := range flagChangeCallbacks {
		if err := verifyFlagChangeFlagName(flagName); nil != err {
			return err
		}
	}
	/* First generation of flags */
	Generation++
	issueAllFlagChangeCallbacks()

	/* Recheck in intervals, if needed */
	go func() {
		for {
			/* Sleep and update if there's an update interval */
			for *configUpdateInterval != 0 {
				// Use time.Sleep() instead of time.Tick() for the sake of dynamic flag update.
				time.Sleep(*configUpdateInterval)
				changes := updateConfig()
				/* Send out the changes, if needed */
				if nil != c {
					go func() { c <- changes }()
				}
			}
			/* Wait to be woke up */
			cond.L.Lock()
			cond.Wait()
			cond.L.Unlock()
		}
	}()

	/* Register to catch SIGHUP */
	ch := make(chan os.Signal)
	signal.Notify(ch, syscall.SIGHUP)
	/* Goroutine to do the catching */
	go func() {
		/* Catch a SIGHUP */
		for _ = range ch {
			/* Update the state */
			changes := updateConfig()
			/* Send out the changes, if needed */
			if nil != c {
				go func() { c <- changes }()
			}
		}
	}()
	return nil
}

// Every time the config file is re-read, an UpdateResult struct is sent out
// via the channel passed to Parse, if the channel is non-nil.

// UpdateResult contains the results of re-reading the config file.  Either
// ChangedFlags or Err will be set, but not both.
type UpdateResult struct {
	ChangedFlags map[string]string /* Flags that changed when the file was read */
	Err          error             /* An error occurred reading the file */
}

/* Re-read the config file and update the state of the flags */
func updateConfig() UpdateResult {
	updateLock.Lock()
	defer updateLock.Unlock()
	/* Parse the new config file, get the old values (or an error) */
	oldFlagValues, err := parseConfigFlags()
	if nil != err {
		return UpdateResult{Err: err}
	}
	/* Return if there's no change */
	if 0 == len(oldFlagValues) {
		return UpdateResult{}
	}

	modifiedFlags := make(map[string]string)
	for k, _ := range oldFlagValues {
		modifiedFlags[k] = flag.Lookup(k).Value.String()
	}
	Generation++
	issueFlagChangeCallbacks(oldFlagValues)
	/* Wake up a sleeping interval watcher */
	if nil != configUpdateInterval {
		cond.L.Lock()
		defer cond.L.Unlock()
		cond.Broadcast()
	}
	return UpdateResult{ChangedFlags: modifiedFlags}
}

// Callback, which is called when the given flag is changed.
//
// The callback may be registered for any flag via OnFlagChange().
type FlagChangeCallback func()

// Registers a callback which is called asynchronously (as go callback())
// after the given flag value is changed.  Flag value can be changed on config
// re-read after catching SIGHUP signal or if periodic config re-read is
// enabled with -configUpdateInterval flag.
//
// Note that flags set via the command line cannot be overriden via config
// file modifications.
func OnFlagChange(flagName string, callback FlagChangeCallback) error {
	if parsed {
		if err := verifyFlagChangeFlagName(flagName); nil != err {
			return err
		}
	}
	/* Add the call back to the appropriate list */
	flagChangeCallbacks[flagName] =
		append(flagChangeCallbacks[flagName], callback)
	return nil
}

func verifyFlagChangeFlagName(flagName string) error {
	if flag.Lookup(flagName) == nil {
		return fmt.Errorf("cannot register callback for "+
			"non-existant flag %v", flagName)
		//		log.Fatalf("iniflags: cannot register FlagChangeCallback for non-existing flag [%s]\n", flagName)
	}
	return nil
}

/* Call the callbacks for the flags that changed */
func issueFlagChangeCallbacks(oldFlagValues map[string]string) {
	/* Iterate through changed flags */
	for flagName := range oldFlagValues {
		/* Check if we have a list of callbacks */
		if fs, ok := flagChangeCallbacks[flagName]; ok {
			/* Call each callback */
			for _, f := range fs {
				go f()
			}
		}
	}
}

/* Call ALL the callbacks */
func issueAllFlagChangeCallbacks() {
	for _, fs := range flagChangeCallbacks {
		for _, f := range fs {
			f()
		}
	}
}

/* Update the variables returned by flag.* with values from the config file
if they weren't specified on the command line */
func parseConfigFlags() (oldFlagValues map[string]string, err error) {
	/* Path to the configuration file */
	configPath := *config
	/* Short-circuit the default */
	if configPath == "" {
		return map[string]string{}, nil
	}
	/* Get the keys and values from the config file */
	parsedArgs, err := getArgsFromConfig(configPath)
	if nil != err {
		return nil, err
	}
	/* Work out which flags weren't specified on the command line */
	missingFlags := getMissingFlags()

	oldFlagValues = make(map[string]string)
	/* Put values in the config file into variables if they weren't
	specified on the command line */
	for _, arg := range parsedArgs {
		/* Make sure the key from the config file is actually a flag */
		f := flag.Lookup(arg.Key)
		if f == nil {
			err = fmt.Errorf("unknown \"%v\" in line %v of "+
				"config file %v",
				arg.Key, arg.LineNum, arg.FilePath)
			break
		}
		/* If the key in the config file wasn't specified on the
		command line, set it in the variable returne by flag.* */
		if _, found := missingFlags[f.Name]; found {
			/* No change if the value from the confige file and the
			value from the command line are the same */
			oldValue := f.Value.String()
			if oldValue == arg.Value {
				continue
			}
			if e := f.Value.Set(arg.Value); err != nil {
				err = fmt.Errorf("unable to set %v to %v, "+
					"from line %v of %v: %v",
					arg.Key, arg.Value,
					arg.LineNum, arg.FilePath, e)
				break
			}
			/* Save the previous value in case we need to
			roll back */
			if oldValue != f.Value.String() {
				oldFlagValues[arg.Key] = oldValue
			}
		}
	}

	/* If we encountered an error, reset the values to what was given on
	the command line */
	if nil != err {
		// restore old flag values
		for k, v := range oldFlagValues {
			flag.Set(k, v)
		}
		oldFlagValues = nil
	}

	return oldFlagValues, err
}

type flagArg struct {
	Key      string
	Value    string
	FilePath string
	LineNum  int
}

/* Extract the key/value pairs from the config file */
func getArgsFromConfig(configPath string) ([]flagArg, error) {
	/* Open the config file */
	file, err := os.Open(configPath)
	if file == nil {
		return nil, err
	}
	defer file.Close()
	r := bufio.NewScanner(file)

	/* Read lines from the config file */
	args := []flagArg{}
	lineNum := 0
	for r.Scan() {
		/* Note where we are in config file */
		lineNum++
		line := r.Text()
		/* Trim trailing and leading spaces */
		line = strings.TrimSpace(line)
		/* Ignore blank lines and comments */
		if "" == line || strings.HasPrefix(line, "#") {
			continue
		}
		/* Split into key and value */
		parts := splitRE.Split(line, 2)
		var key, value string /* Key and value from config file */
		key = strings.TrimSpace(parts[0])
		/* If the value isn't specified, hope it's a boolean */
		if 1 == len(parts) {
			value = "true"
		} else {
			value = parts[1]
		}
		/* Not that we have the flag */
		args = append(args, flagArg{
			Key:      key,
			Value:    value,
			FilePath: file.Name(),
			LineNum:  lineNum,
		})
	}
	/* Scanner error? */
	if err := r.Err(); nil != err {
		return nil, err
	}

	return args, nil
}

/* getMissingFlags returns a hash of flags which were not specified on the
command line */
func getMissingFlags() map[string]bool {
	/* Work out which flags have been set on the command line */
	setFlags := make(map[string]bool)
	flag.Visit(func(f *flag.Flag) {
		setFlags[f.Name] = true
	})

	/* Work out which flags haven't */
	missingFlags := make(map[string]bool)
	flag.VisitAll(func(f *flag.Flag) {
		if _, ok := setFlags[f.Name]; !ok {
			missingFlags[f.Name] = true
		}
	})
	return missingFlags
}

/* Print the current state of the flags (key/value pairs) in ini format */
func dumpFlags() {
	flag.VisitAll(func(f *flag.Flag) {
		if f.Name != "config" && f.Name != "dumpflags" {
			fmt.Printf("# %s\n", strings.Replace(
				strings.Replace(f.Usage, "\r\n", "\n", -1),
				"\n", "\n#\t", -1))
			fmt.Printf("%s %s\n", f.Name, f.Value.String())
		}
	})
}
