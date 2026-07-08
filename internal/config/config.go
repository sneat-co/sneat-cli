package config

const (
	DefaultProject               = "sneat-eur3-1"
	DefaultAPIKey                = "AIzaSyCeQu1WC182yD0VHrRm4nHUxVf27fY-MLQ"
	DefaultAuthDomain            = "sneat-eur3-1.firebaseapp.com"
	DefaultAPIBaseURL            = "https://api.sneat.cloud/v0/"
	DefaultAuthEmulatorHost      = "localhost:9099"
	DefaultFirestoreEmulatorHost = "localhost:8080"
)

// Config is the resolved runtime configuration.
type Config struct {
	Project               string
	APIKey                string
	AuthDomain            string
	APIBaseURL            string
	AuthEmulatorHost      string
	FirestoreEmulatorHost string
}

// Overrides are the flag-supplied values (empty string / false = not set).
type Overrides struct {
	Project               string
	APIKey                string
	AuthDomain            string
	APIBaseURL            string
	AuthEmulatorHost      string
	FirestoreEmulatorHost string
	Emulator              bool
}

func pick(flag, env, def string) string {
	if flag != "" {
		return flag
	}
	if env != "" {
		return env
	}
	return def
}

// Resolve applies precedence flag > env > default for each field.
func Resolve(o Overrides, getenv func(string) string) Config {
	c := Config{
		Project:               pick(o.Project, getenv("SNEAT_FIREBASE_PROJECT"), DefaultProject),
		APIKey:                pick(o.APIKey, getenv("FIREBASE_API_KEY"), DefaultAPIKey),
		AuthDomain:            pick(o.AuthDomain, getenv("FIREBASE_AUTH_DOMAIN"), DefaultAuthDomain),
		APIBaseURL:            pick(o.APIBaseURL, getenv("SNEAT_API_BASE_URL"), DefaultAPIBaseURL),
		AuthEmulatorHost:      pick(o.AuthEmulatorHost, getenv("FIREBASE_AUTH_EMULATOR_HOST"), ""),
		FirestoreEmulatorHost: pick(o.FirestoreEmulatorHost, getenv("FIRESTORE_EMULATOR_HOST"), ""),
	}
	if o.Emulator {
		if c.AuthEmulatorHost == "" {
			c.AuthEmulatorHost = DefaultAuthEmulatorHost
		}
		if c.FirestoreEmulatorHost == "" {
			c.FirestoreEmulatorHost = DefaultFirestoreEmulatorHost
		}
	}
	return c
}
