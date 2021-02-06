package eredes

// TODOs:
// 1 Add retry logic (after 1h for N attempts) if error, timeout or no results
// 2 When using start date, use only in first request, then history interval
// 3 Store last successful date and use that if retries failed

import (
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/plugins/common/tls"
	"github.com/influxdata/telegraf/plugins/inputs"
	"github.com/influxdata/telegraf/plugins/parsers"
	"github.com/tidwall/gjson"
)

// EREDES struct
type EREDES struct {
	Headers map[string]string `toml:"headers"`

	SignInURL string `toml:"sign_in_url"`
	UsageURL  string `toml:"usage_url"`

	Username string `toml:"username"`
	Password string `toml:"password"`
	Cpe      string `toml:"cpe"`

	tls.ClientConfig

	SuccessStatusCodes []int `toml:"success_status_codes"`

	Timeout internal.Duration `toml:"timeout"`

	HistoryInterval internal.Duration `toml:"history_interval"`

	StartDate string `toml:"start_date"`

	RunTestsOnly bool `toml:"run_tests_only"`

	client *http.Client

	// The parser will automatically be set by Telegraf core code because
	// this plugin implements the ParserInput interface (i.e. the SetParser method)
	parser parsers.Parser
}

var eredesSignIn = "https://online.e-redes.pt/listeners/api.php/ms/auth/auth/signin"
var eredesUsage = "https://online.e-redes.pt/listeners/api.php/ms/reading/data-usage/sysgrid/get"

var sampleConfig = `
  ## E-Redes Auth Credentials
  # username = "username"
  # password = "password"
  # cpe = "cpe"

  # sign_in_url = "https://online.e-redes.pt/listeners/api.php/ms/auth/auth/signin"
  # usage_url = "https://online.e-redes.pt/listeners/api.php/ms/reading/data-usage/sysgrid/get"
  # insecure_skip_verify = true

  ## Amount of time allowed to complete the HTTP request (default is 60s)
  # timeout = "60s"

  # Interval to request until start of current day
  # Minimum is 24h
  # Ex: 24h = last 24h = yesterday 00:00 to 23:59
  # E-Redes doesn't provide realtime (current day) readings at the time
  # history_interval = "24h"

  # If range is defined, first request will fetch this range and then
  # proceed with interval
  # start_date = "2020-12-31 23:59:59"
`

// SampleConfig returns the default configuration of the Input
func (*EREDES) SampleConfig() string {
	return sampleConfig
}

// Description returns a one-sentence description on the Input
func (*EREDES) Description() string {
	return "Read formatted metrics from E-Redes"
}

// Init init method
func (eredes *EREDES) Init() error {
	tlsCfg, err := eredes.ClientConfig.TLSConfig()

	if err != nil {
		return err
	}

	transport := &http.Transport{
		TLSClientConfig: tlsCfg,
	}

	eredes.client = &http.Client{
		Transport: transport,
		Timeout:   eredes.Timeout.Duration,
	}

	eredes.SuccessStatusCodes = []int{200}
	return nil
}

// Gather takes in an accumulator and adds the metrics that the Input
// gathers. This is called every "interval"
func (eredes *EREDES) Gather(acc telegraf.Accumulator) error {
	token, err := eredes.signIn()
	if err != nil {
		acc.AddError(fmt.Errorf("[signIn]: %s", err))
		return nil
	}

	if token != "" {
		err = eredes.gatherUsages(acc, token)
		if err != nil {
			acc.AddError(fmt.Errorf("Error in : %s", err))
			return nil
		}
	}

	return nil
}

// SetParser takes the data_format from the config and finds the right parser for that format
func (eredes *EREDES) SetParser(parser parsers.Parser) {
	eredes.parser = parser
}

// Gathers data from a particular URL
// Parameters:
//     acc    : The telegraf Accumulator to use
//     url    : endpoint to send request to
//
// Returns:
//     error: Any error that may have occurred
func (eredes *EREDES) gatherUsages(
	acc telegraf.Accumulator,
	token string,
) error {

	log.Printf("[eredes] starting")

	var start string = ""
	var end string = ""
	historyInterval := eredes.HistoryInterval.Duration
	var twentyFourHours time.Duration = 24 * time.Hour
	startDate := time.Now()

	//Note: start date is exclusive, so 00:00:00 won't be included in the request.

	if eredes.StartDate == "" {
		log.Printf("[eredes] no start date defined")

		if historyInterval == 0 || historyInterval < twentyFourHours {
			log.Printf("[eredes] no history interval defined or < 24h, using 24h")
			startDate = startDate.Add(-twentyFourHours)
			startDate = time.Date(startDate.Year(), startDate.Month(), startDate.Day(), 23, 59, 59, 0, startDate.Location())
		} else {
			startDate = startDate.Add(-historyInterval).Add(-twentyFourHours)
			startDate = time.Date(startDate.Year(), startDate.Month(), startDate.Day(), 23, 59, 59, 0, startDate.Location())
		}
		start = startDate.Format("2006-01-02 15:04:05")
	} else {
		start = eredes.StartDate
	}

	endDate := time.Now().Add(-twentyFourHours)
	endDate = time.Date(endDate.Year(), endDate.Month(), endDate.Day(), 23, 59, 59, 59, endDate.Location())
	end = endDate.Format("2006-01-02 15:04:05")

	log.Printf("[eredes] start date: " + start + " end date: " + end)

	var usagesRequestBody string = `{"cpe": "` + eredes.Cpe + `", "request_type":"3","start_date":"` + start + `","end_date":"` + end + `","wait":true,"formatted":false}`

	usageURL := eredes.UsageURL

	if usageURL == "" {
		usageURL = eredesUsage
	}

	// log.Printf("[eredes] request URL: " + usageURL)
	// log.Printf("[eredes] request body: " + usagesRequestBody)

	if !eredes.RunTestsOnly {
		log.Printf("[eredes] requesting usages")
		response, err := eredes.makeRequest(eredesUsage, usagesRequestBody, token)
		if err != nil {
			return err
		}

		// log.Printf("[eredes] response:")
		// log.Printf(string(response))

		metrics, err := eredes.parser.Parse(response)
		if err != nil {
			return err
		}

		if len(metrics) > 0 {
			log.Printf("[eredes] adding %d metrics", len(metrics))
			for _, metric := range metrics {
				acc.AddFields(metric.Name(), metric.Fields(), metric.Tags(), metric.Time())
			}
		} else {
			log.Printf("[eredes] no metrics to add")
		}

	}

	return nil
}

// Sign in to E-Redes
// Parameters:
// Returns:
//	   token: The authentication token
//     error: Any error that may have occurred
func (eredes *EREDES) signIn() (string, error) {
	if eredes.RunTestsOnly {
		return "TOKEN1234567890", nil
	}

	signInURL := eredes.SignInURL

	if signInURL == "" {
		signInURL = eredesSignIn
	}

	log.Printf("[eredes] login")
	signInRequestBody := `{"password": "` + eredes.Password + `", "username": "` + eredes.Username + `"}`
	// log.Printf("[signIn] request URL: " + signInURL)
	// log.Printf("[signIn] request body: " + signInRequestBody)

	response, err := eredes.makeRequest(signInURL, signInRequestBody, "")
	if err != nil {
		log.Printf("[eredes] error login")
		return "", err
	}

	// log.Printf("[eredes] response:")
	// log.Printf(string(response))
	log.Printf("[eredes] login successful")
	token := gjson.Get(string(response), "Body.Result.token")

	return token.String(), nil
}

// Make request to a particular URL
// Parameters:
//     url    : endpoint to send request to
//
// Returns:
//	   response: The parsed response
//     error: Any error that may have occurred
func (eredes *EREDES) makeRequest(
	url string,
	requestBody string,
	token string,
) ([]byte, error) {
	body, err := makeRequestBodyReader(requestBody)
	if err != nil {
		return nil, err
	}
	defer body.Close()

	request, err := http.NewRequest("POST", url, body)
	if err != nil {
		return nil, err
	}

	for k, v := range eredes.Headers {
		if strings.ToLower(k) == "host" {
			request.Host = v
		} else {
			request.Header.Add(k, v)
		}
	}

	if token != "" {
		bearer := "Bearer " + strings.Trim(string(token), "\n")
		request.Header.Set("Authorization", bearer)
	}

	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_13_6) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/13.1.2 Safari/605.1.15")

	resp, err := eredes.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	responseHasSuccessCode := false
	for _, statusCode := range eredes.SuccessStatusCodes {
		if resp.StatusCode == statusCode {
			responseHasSuccessCode = true
			break
		}
	}

	if !responseHasSuccessCode {
		return nil, fmt.Errorf("received status code %d (%s), expected any value out of %v",
			resp.StatusCode,
			http.StatusText(resp.StatusCode),
			eredes.SuccessStatusCodes)
	}

	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return b, nil
}

func makeRequestBodyReader(body string) (io.ReadCloser, error) {
	var reader io.Reader = strings.NewReader(body)
	return ioutil.NopCloser(reader), nil
}

func init() {
	inputs.Add("eredes", func() telegraf.Input {
		return &EREDES{
			Timeout: internal.Duration{Duration: time.Second * 120},
		}
	})
}
