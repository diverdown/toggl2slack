package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"github.com/VividCortex/godaemon"
	"github.com/en30/toggl"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"text/template"
)

type Config struct {
	Interval    int64              `json:"interval"`
	TogglToken  string             `json:"toggl_token"`
	DashboardId int                `json:"dashboard_id"`
	WebhookURL  string             `json:"webhook_url"`
	Users       map[string]Payload `json:"users"`
	Templates   Templates          `json:"templates"`
	LogFile     string             `json:"log_file"`
}

type Templates struct {
	Started  *template.Template
	Finished *template.Template
}

func (t *Templates) UnmarshalJSON(data []byte) error {
	var j map[string]string
	if err := json.Unmarshal(data, &j); err != nil {
		return err
	}
	t.Started = template.Must(template.New("started").Parse(j["started"]))
	t.Finished = template.Must(template.New("finished").Parse(j["finished"]))
	return nil
}

type Payload struct {
	Channel   string `json:"channel"`
	IconEmoji string `json:"icon_emoji,omitempty"`
	IconUrl   string `json:"icon_url,omitempty"`
	Username  string `json:"username"`
	Text      string `json:"text"`
}

func (p *Payload) reverseMergeDefault() {
	if p.Channel == "" {
		p.Channel = "#general"
	}
	if p.Username == "" {
		p.Username = "Toggl"
	}
	if p.IconEmoji == "" && p.IconUrl == "" {
		p.IconUrl = "http://blog.toggl.com/wp-content/uploads/2015/04/toggl-button-light.png"
	}
	if p.IconEmoji != "" && p.IconUrl != "" {
		panic("Do not specify both icon_emoji and icon_url")
	}
}

func loadConfig(path *string) (*Config, error) {
	file, err := os.Open(*path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	config := &Config{}
	res, err := ioutil.ReadAll(file)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(res, config)
	if err != nil {
		return nil, err
	}
	return config, nil
}

func notify(c *Config, t *template.Template, a *toggl.Activity, p Payload) error {
	buf := &bytes.Buffer{}
	t.Execute(buf, a)
	p.Text = buf.String()

	p.reverseMergeDefault()
	client := &http.Client{}
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	r := bytes.NewReader(b)
	req, err := http.NewRequest("POST", c.WebhookURL, r)
	if err != nil {
		return err
	}
	req.Header.Add("Content-Type", "application/json")

	log.Printf("%v %v %v\n", req.Proto, req.Method, req.Host)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	log.Printf("%v %v\n", resp.Proto, resp.Status)

	return err
}

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)
	c := flag.String("config", "config.json", "configuration file path")
	d := flag.Bool("daemonize", false, "daemonize")
	flag.Parse()
	config, err := loadConfig(c)
	if err != nil {
		log.Fatal(err)
	}

	openFiles := make([]**os.File, 0)

	if config.LogFile != "" {
		path, err := filepath.Abs(config.LogFile)
		if err != nil {
			log.Fatal(err)
		}
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0644)
		if err != nil {
			log.Fatal(err)
		}
		openFiles = append(openFiles, &file)
		err = syscall.Dup2(int(file.Fd()), int(os.Stderr.Fd()))
		if err != nil {
			log.Fatal(err)
		}
		log.SetOutput(file)

	}

	if *d {
		godaemon.MakeDaemon(&godaemon.DaemonAttr{ProgramName: "toggl2slack", CaptureOutput: true, Files: openFiles})
	}

	onStart := func(a *toggl.Activity) {
		if p, ok := config.Users[strconv.Itoa(a.UserId)]; ok {
			err = notify(config, config.Templates.Started, a, p)
			if err != nil {
				log.Println("ERROR[toggl2slack]: ", err)
			}
		}
	}
	onStop := func(a *toggl.Activity) {
		if p, ok := config.Users[strconv.Itoa(a.UserId)]; ok {
			err = notify(config, config.Templates.Finished, a, p)
			if err != nil {
				log.Println("ERROR[toggl2slack]: ", err)
			}
		}
	}
	onError := func(e error) {
		log.Println("Error[toggl2slack]: ", e)
	}

	err = toggl.NewHook(config.Interval, config.DashboardId, config.TogglToken, onStart, onStop, onError)
	if err != nil {
		log.Fatal(err)
	}
	select {}
}
