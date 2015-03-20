// Package autoupdate provides Lantern with special tools to autoupdate itself
// with minimal effort.
package autoupdate

import (
	"net/http"
	"strings"
	"time"

	"github.com/getlantern/flashlight/util"
	"github.com/getlantern/go-update"
	"github.com/getlantern/go-update/check"
	"github.com/getlantern/golog"
)

var log = golog.LoggerFor("autoupdate")

// Making sure AutoUpdate and Patch satisfy AutoUpdater and Patcher.
var (
	_ = AutoUpdater(&AutoUpdate{})
	_ = Patcher(&Patch{})
)

var (
	// How much time should we wait between update attempts?
	sleepTime = time.Hour * 4
)

type Config struct {
	URL       string
	PublicKey []byte
}

// SetProxy sets the proxy to use.
func SetProxy(proxyAddr string) {
	var err error

	if proxyAddr != "" {
		// Create a HTTP proxy and pass it to the update package.
		if update.HTTPClient, err = util.HTTPClient("", proxyAddr); err != nil {
			log.Errorf("Could not use proxy: %q\n", err)
		}
	} else {
		update.HTTPClient = &http.Client{}
	}
}

// AutoUpdate satisfies AutoUpdater and can be used for other programs to
// configure automatic updates.
type AutoUpdate struct {
	*Config
	v string
	// When a patch has been applied, the patch's version will be sent to
	// UpdatedTo.
	UpdatedTo chan string
}

// New creates an AutoUpdate struct based on the given *Config.
func New(cfg *Config) *AutoUpdate {
	a := &AutoUpdate{
		UpdatedTo: make(chan string),
		Config:    cfg,
	}
	return a
}

// SetVersion sets the version of the process' executable file.
func (a *AutoUpdate) SetVersion(v string) {
	if !strings.HasPrefix(v, "v") {
		// Panicking because versions must begin with "v".
		panic(`autoupdate: Versions must begin with a "v".`)
	}
	if !isVersionTag(v) {
		panic(`autoupdate: Versions must be in the form vX.Y.Z.`)
	}
	a.v = v
}

// Version returns the internal version value passed to SetVersion(). If
// SetVersion() has not been called yet, a negative value will be returned
// instead.
func (a *AutoUpdate) Version() string {
	return a.v
}

// check uses go-update to look for updates.
func (a *AutoUpdate) check() (res *check.Result, err error) {
	var up *update.Update

	param := check.Params{
		AppVersion: a.Version(),
	}

	up = update.New().ApplyPatch(update.PATCHTYPE_BSDIFF)

	if _, err = up.VerifySignatureWithPEM(a.PublicKey); err != nil {
		return nil, err
	}

	if res, err = param.CheckForUpdate(a.URL, up); err != nil {
		if err == check.NoUpdateAvailable {
			return nil, nil
		}
		return nil, err
	}

	return res, nil
}

// Query checks if a new version is available and returns a Patcher.
func (a *AutoUpdate) Query() (Patcher, error) {
	var res *check.Result
	var err error

	if res, err = a.check(); err != nil {
		return nil, err
	}

	if res == nil {
		// No new version is available.
		return &Patch{}, nil
	}

	return &Patch{res: res, v: res.Version}, nil
}

func (a *AutoUpdate) loop() {
	log.Debug("Starting to watch for updates")
	for {
		patch, err := a.Query()

		if err == nil {
			log.Debugf("Old: %v  New: %v", a.Version(), patch.Version())
			if VersionCompare(a.Version(), patch.Version()) == Higher {
				log.Debugf("autoupdate: Attempting to update to %s.", patch.Version())

				err = patch.Apply()

				if err == nil {
					log.Debugf("autoupdate: Patching succeeded!")
					// Updating version.
					a.UpdatedTo <- patch.Version()
					a.SetVersion(patch.Version())
				} else {
					log.Errorf("autoupdate: Patching failed: %q\n", err)
				}

			} else {
				log.Debug("autoupdate: Already up to date.")
			}
		} else {
			log.Debugf("autoupdate: Could not reach update server: %q\n", err)
		}

		time.Sleep(sleepTime)
	}
}

// Watch spawns a goroutine that will apply updates whenever they're available.
func (a *AutoUpdate) Watch() {
	if a.v == "" {
		// Panicking because Watch is useless without the ability to compare
		// versions.
		panic(`autoupdate: You must set the executable version in order to watch for updates!`)
	}
	go a.loop()
}
