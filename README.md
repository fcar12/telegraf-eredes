# telegraf-eredes

Input plugin to collect metrics (power consumption) from E-Redes.

### Compile telegraf with Eredes support:

1. Download telegraf from [repository](https://github.com/influxdata/telegraf). 
2. Copy `eredes` to `plugins/inputs` directory.
3. All eredes entry to `plugins/inputs/all/all.go` (follow the format used in other plugins listed).
4. Compile telegraf. Follow instructions from telegraf repository, but in short just run `make`. If compiling for linux (ex: docker), set arch before make with `export GOOS=linux`; for mac `export GOOS=darwin`.

**Note**: if running into issues with ssl certificates, set `insecure_skip_verify = true` in configuration.

### Sample Configuration:

```toml
[[inputs.redes]]
  ## E-Redes Auth Credentials
  # username = "username"
  # password = "password"
  # cpe = "cpe"

  # E-Redes sign in and consumptions URLs (default is the configured below)
  # sign_in_url = "https://online.e-redes.pt/listeners/api.php/ms/auth/auth/signin"
  # usage_url = "https://online.e-redes.pt/listeners/api.php/ms/reading/data-usage/sysgrid/get"
  # insecure_skip_verify = true

  ## Amount of time allowed to complete the HTTP request (default is 60s)
  # timeout = "60s"

  # Interval to request until start of current day
  # Minimum is 24h
  # Ex: 24h = last 24h = yesterday 00:00 to 23:59
  # E-Redes doesn't provide realtime (current day) readings at the time
  history_interval = "168h" # 1 week

  # If start date is defined, history_interval is ignored
  # start_date = "2020-12-31 23:59:59"

  interval = "12h"
  
  # Parser configuration, configured for current state of E-Redes endpoints
  data_format = "json"
  json_query = "Body.Result.utilitiesDevices.0.meterLoadCurves.0.loadCurves"
  json_name_key = "edp_dist"
  json_time_key = "loadCurveTimestamp"
  json_time_format = "2006-01-02T15:04:05Z"
  json_string_fields = ["meterLoadCurve"]

# Optional, format that for influx measurement
[[processors.converter]]
  order = 1
  [processors.converter.fields]
    float = ["meterLoadCurve"]

[[processors.rename]]
  order = 2
  [[processors.rename.replace]]
    field = "meterLoadCurve"
    dest = "value"
```