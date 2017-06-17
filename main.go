package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Masterminds/sprig"
	"github.com/en30/toggl"
	"github.com/urfave/cli"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"text/template"
)

type Config struct {
	Interval    int64              `json:"interval"`
	TogglToken  string             `json:"toggl_token"`
	DashboardId int                `json:"dashboard_id"`
	WebhookURL  string             `json:"webhook_url"`
	Users       map[string]Payload `json:"users"`
	Templates   Templates          `json:"templates"`
}

type Templates struct {
	Started  *template.Template
	Finished *template.Template
}

func (t Templates) MarshalJSON() ([]byte, error) {
	if t.Started == nil && t.Finished == nil {
		return json.Marshal(map[string]string{
			"started":  "started {{.Description}}",
			"finished": "finished {{.Description}}",
		})
	}
	return []byte{}, nil
}

func (t *Templates) UnmarshalJSON(data []byte) error {
	var j map[string]string
	if err := json.Unmarshal(data, &j); err != nil {
		return err
	}
	t.Started = template.Must(template.New("started").Funcs(sprig.TxtFuncMap()).Parse(j["started"]))
	t.Finished = template.Must(template.New("finished").Funcs(sprig.TxtFuncMap()).Parse(j["finished"]))
	return nil
}

type Payload struct {
	Channel   string `json:"channel"`
	IconEmoji string `json:"icon_emoji,omitempty"`
	IconUrl   string `json:"icon_url,omitempty"`
	Username  string `json:"username"`
	Text      string `json:"text,omitempty"`
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

func loadConfig(path string) (*Config, error) {
	file, err := os.Open(path)
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

func configPath(c *cli.Context) string {
	p := c.String("config")
	if p == "" {
		return "config.json"
	} else {
		return p
	}
}

func generateConfig(c *cli.Context) error {
	path := configPath(c)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		defaultConfig := Config{
			Interval:   60,
			TogglToken: "YOUR_TOGGL_TOKEN",
			Templates:  Templates{},
			WebhookURL: "https://hooks.slack.com/services/...",
			Users:      map[string]Payload{"TOGGL_USER_ID": Payload{Channel: "#general", Username: "toggl2slack"}},
		}
		config, err := json.MarshalIndent(defaultConfig, "", "    ")
		if err != nil {
			return err
		}
		err = ioutil.WriteFile(path, config, 0644)
		if err == nil {
			fmt.Printf("%v was generated\n", path)
		}
		return err
	} else {
		return errors.New("config.json already exists")
	}
}

func start(con *cli.Context) error {
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)
	c := configPath(con)
	config, err := loadConfig(c)
	if err != nil {
		log.Fatal(err)
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

func main() {
	app := cli.NewApp()
	app.Name = "toggl2slack"
	app.HelpName = "toggl2slack"
	app.Usage = "notify Toggl activities to Slack"
	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:  "config",
			Value: "config.json",
			Usage: "config file",
		},
	}
	app.Commands = []cli.Command{
		{
			Name:    "init",
			Aliases: []string{"g", "generate", "i"},
			Usage:   "generate a config file",
			Action:  generateConfig,
		},
		{
			Name:    "start",
			Aliases: []string{"s"},
			Usage:   "start toggl2slack",
			Action:  start,
		},
	}
	app.Run(os.Args)
}
