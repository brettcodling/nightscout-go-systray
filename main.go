package main

import (
	"encoding/base64"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"log/syslog"
	"math"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/boltdb/bolt"
	"github.com/gen2brain/beeep"
	"github.com/getlantern/systray"
)

const mgdltommol = 18.018018018
const predictLowSeconds = 3600

type bg struct {
	Direction          direction
	LastBgAlert        string
	LastDirectionAlert string
	PreviousValue      bgValue
	Value              bgValue
}

type bgValue struct {
	Timestamp int64
	Value     float64
}

type direction struct {
	Value      string
	IsRising   bool
	IsFalling  bool
	IsFallback bool
}

type flags struct {
	Url        *string
	Urgenthigh *float64
	High       *float64
	Low        *float64
}

type icon struct {
	Base64  string
	Decoded []byte
}

var (
	alertValues = map[string]bool{}
	alertKeys   = []string{
		"Predicted low",
		"Low",
		"Falling fast",
		"Urgent high",
		"Rising fast",
	}
	args = flags{
		Url:        flag.String("url", "", "Your nightscout url e.g. https://example.herokuapp.com"),
		Urgenthigh: flag.Float64("urgent-high", 15.0, "Your BG urgent high target"),
		High:       flag.Float64("high", 8.0, "Your BG high target"),
		Low:        flag.Float64("low", 4.0, "Your BG low target"),
	}
	currentBg  *systray.MenuItem
	directions = map[string]direction{
		"TripleUp": {
			Value:      "⤊",
			IsRising:   true,
			IsFalling:  false,
			IsFallback: false,
		},
		"DoubleUp": {
			Value:      "⇈",
			IsRising:   true,
			IsFalling:  false,
			IsFallback: false,
		},
		"SingleUp": {
			Value:      "↑",
			IsRising:   true,
			IsFalling:  false,
			IsFallback: false,
		},
		"FortyFiveUp": {
			Value:      "↗",
			IsRising:   false,
			IsFalling:  false,
			IsFallback: false,
		},
		"Flat": {
			Value:      "→",
			IsRising:   false,
			IsFalling:  false,
			IsFallback: false,
		},
		"FortyFiveDown": {
			Value:      "↘",
			IsRising:   false,
			IsFalling:  false,
			IsFallback: false,
		},
		"SingleDown": {
			Value:      "↓",
			IsRising:   false,
			IsFalling:  true,
			IsFallback: false,
		},
		"DoubleDown": {
			Value:      "⇊",
			IsRising:   false,
			IsFalling:  true,
			IsFallback: false,
		},
		"TripleDown": {
			Value:      "⤋",
			IsRising:   false,
			IsFalling:  true,
			IsFallback: false,
		},
		"None": {
			Value:      "-",
			IsRising:   false,
			IsFalling:  false,
			IsFallback: true,
		},
	}
	fallbackDirection = "None"
	icons             = map[string]icon{
		"red": {
			Base64: "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",
		},
		"green": {
			Base64: "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",
		},
		"orange": {
			Base64: "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",
		},
	}
	inRangeAt   *systray.MenuItem
	inRangeTime time.Time
	lastBg      bg
	lowAt       *systray.MenuItem
	lowTime     time.Time
	previousBg  *systray.MenuItem
	showBg      bool
)

func main() {
	syslog, err := syslog.New(syslog.LOG_INFO, "CGM")
	if err != nil {
		panic("Unable to connect to syslog")
	}
	log.SetOutput(syslog)

	flag.Parse()

	if *args.Url == "" {
		log.Fatal("A nightscout URL is required")
	}

	db, err := bolt.Open("cgm.db", 0600, nil)
	if err != nil {
		log.Fatal("Failed to initialse DB")
	}
	defer db.Close()

	err = db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte("keys"))
		if err != nil {
			return err
		}
		v := b.Get([]byte("showBg"))
		if len(v) == 0 {
			v = []byte("true")
		}
		showBg = (string(v) == "true")
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}

	systray.Run(func() {
		currentBg = systray.AddMenuItem("", "")
		if showBg {
			currentBg.Hide()
		}
		inRangeAt = systray.AddMenuItem("", "")
		inRangeAt.Hide()
		lowAt = systray.AddMenuItem("", "")
		lowAt.Hide()
		previousBg = systray.AddMenuItem("", "")
		previousBg.Hide()
		open := systray.AddMenuItem("Open in browser", "")
		refresh := systray.AddMenuItem("Refresh", "")
		addAlertSettings(db)
		showCurrent := systray.AddMenuItemCheckbox("Show current value", "", showBg)
		quit := systray.AddMenuItem("Quit", "")
		go func() {
			for {
				select {
				case <-open.ClickedCh:
					exec.Command("xdg-open", *args.Url).Start()
				case <-refresh.ClickedCh:
					setBg()
				case <-showCurrent.ClickedCh:
					toggleShowCurrent(showCurrent, db)
				case <-quit.ClickedCh:
					systray.Quit()
				}
			}
		}()
		setBg()
		for range time.Tick(time.Minute * 1) {
			setBg()
		}
	}, func() {})
}

func (b *bg) alert() {
	alerts := b.getAlerts()
	if len(alerts) < 1 {
		return
	}
	var filename string
	file, err := ioutil.TempFile("./", "red.png")
	if err == nil {
		defer file.Close()
		img, err := decodedIcon("red")
		if err != nil {
			log.Println(err)
		} else {
			file.Write(img)
			filename = file.Name()
		}
	}

	for _, alert := range alerts {
		beeep.Alert("CGM", alert, filename)
	}
	if filename != "" {
		os.Remove(filename)
	}
}

func (b *bg) format() string {
	return fmt.Sprintf("%.1f %s", b.Value.Value, b.Direction.Value)
}

func (b *bg) getAlerts() (alerts []string) {
	if b.Direction.IsFallback {
		if b.LastDirectionAlert != "failed" {
			alerts = append(alerts, fmt.Sprintf("Failed to get BG direction. %.1f", b.Value.Value))
			b.LastDirectionAlert = "failed"
		}
	} else if b.Value.Value != b.PreviousValue.Value {
		if b.Direction.IsRising {
			if b.LastDirectionAlert != "rising" {
				if alertValues["Rising fast"] {
					alerts = append(alerts, fmt.Sprintf("Rising fast! %.1f %s", b.Value.Value, b.Direction.Value))
					b.LastDirectionAlert = "rising"
				}
			}
		} else if b.Direction.IsFalling {
			if b.LastDirectionAlert != "falling" {
				if alertValues["Falling fast"] {
					alerts = append(alerts, fmt.Sprintf("Falling fast! %.1f %s", b.Value.Value, b.Direction.Value))
					b.LastDirectionAlert = "falling"
				}
			}
		}
	}

	if b.Value.isLow() {
		if b.LastBgAlert != "low" {
			if alertValues["Low"] {
				alerts = append(alerts, fmt.Sprintf("Low! %.1f %s", b.Value.Value, b.Direction.Value))
				b.LastBgAlert = "low"
			}
		}
	} else if b.Value.isUrgentHigh() {
		if b.LastBgAlert != "high" {
			if alertValues["Urgent High"] {
				alerts = append(alerts, fmt.Sprintf("Urgent Hight! %.1f %s", b.Value.Value, b.Direction.Value))
				b.LastBgAlert = "high"
			}
		}
	}

	if lowTime.After(time.Now()) && lowTime.Before(time.Now().Add(predictLowSeconds*time.Second)) {
		if alertValues["Predicted low"] {
			alerts = append(alerts, fmt.Sprintf("Predicted Low at %s!", lowTime.Format("15:04")))
		}
		lowAt.SetTitle(fmt.Sprintf("Low at: %s", lowTime.Format("15:04")))
		lowAt.Show()
	} else if b.Value.Timestamp != b.PreviousValue.Timestamp {
		lowAt.Hide()
	}

	return
}

func (b *bg) getBg() string {
	url := *args.Url + "/api/v1/entries?count=1"
	resp, err := http.Get(url)
	if err != nil {
		log.Println(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Println(err)
	}
	stringBody := strings.Split(string(body), "\t")
	timestamp, err := strconv.ParseInt(stringBody[1], 10, 64)
	if err != nil {
		log.Println(err)
	}
	mgdl, err := strconv.Atoi(stringBody[2])
	if err != nil {
		log.Println(err)
	}
	direction := strings.ReplaceAll(stringBody[3], "\"", "")
	_, ok := directions[direction]
	if !ok {
		direction = fallbackDirection
	}

	if b.Value.Timestamp > 0 && b.Value.Timestamp != timestamp {
		previousBg.SetTitle(fmt.Sprintf("Previous bg: %.1f %s", b.Value.Value, b.Direction.Value))
	}

	b.Direction = directions[direction]
	b.PreviousValue = b.Value
	b.Value = bgValue{
		Timestamp: timestamp,
		Value:     float64(mgdl) / mgdltommol,
	}
	b.calculateLowTime()
	b.alert()
	b.calculateInRangeTime()

	if inRangeTime.After(time.Now()) {
		inRangeAt.SetTitle(fmt.Sprintf("In range at: %s", inRangeTime.Format("15:04")))
		inRangeAt.Show()
	} else if b.Value.Timestamp != b.PreviousValue.Timestamp {
		inRangeAt.Hide()
	}

	return b.format()
}

func (b bg) getIcon() []byte {
	var i string
	if b.Value.isLow() || b.Value.isUrgentHigh() {
		i = "red"
	} else if b.Value.isHigh() {
		i = "orange"
	} else {
		i = "green"
	}

	img, err := decodedIcon(i)
	if err != nil {
		log.Println(err)
	}

	return img
}

func (b bg) calculateLowTime() {
	if b.Value.Timestamp != b.PreviousValue.Timestamp {
		var secondsToLow int
		if b.Value.Value < b.PreviousValue.Value {
			seconds := (b.Value.Timestamp - b.PreviousValue.Timestamp) / 1000
			changePerSecond := (b.PreviousValue.Value - b.Value.Value) / float64(seconds)
			secondsToLow = int((b.Value.Value - *args.Low) / changePerSecond)
			lowTime = time.Now().Add(time.Duration(secondsToLow) * time.Second)
		} else {
			lowTime = time.Now()
		}
	}
}

func (b bg) calculateInRangeTime() {
	if b.Value.Timestamp != b.PreviousValue.Timestamp {
		seconds := math.Abs(float64((b.Value.Timestamp - b.PreviousValue.Timestamp) / 1000))
		changePerSecond := math.Abs((b.PreviousValue.Value - b.Value.Value) / seconds)
		var secondsToInRange int
		if b.Value.isHigh() && b.Value.Value < b.PreviousValue.Value {
			secondsToInRange = int((b.Value.Value - *args.High) / changePerSecond)
		} else if b.Value.isLow() && b.Value.Value > b.PreviousValue.Value {
			secondsToInRange = int((*args.Low - b.Value.Value) / changePerSecond)
		}
		inRangeTime = time.Now().Add(time.Duration(secondsToInRange) * time.Second)
	}
}

func (b bgValue) isUrgentHigh() bool {
	return b.Value >= *args.Urgenthigh
}

func (b bgValue) isHigh() bool {
	return b.Value >= *args.High
}

func (b bgValue) isLow() bool {
	return b.Value < *args.Low
}

func addAlertSettings(db *bolt.DB) {
	alerts := systray.AddMenuItem("Alerts", "")
	db.Batch(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte("alerts"))
		if err != nil {
			return err
		}
		for _, alert := range alertKeys {
			v := b.Get([]byte(alert))
			if len(v) == 0 {
				v = []byte("true")
			}
			alertValues[alert] = (string(v) == "true")
			a := alerts.AddSubMenuItemCheckbox(alert, "", alertValues[alert])
			go func(alert string) {
				for {
					select {
					case <-a.ClickedCh:
						if a.Checked() {
							a.Uncheck()
						} else {
							a.Check()
						}
						alertValues[alert] = a.Checked()
						db.Update(func(tx *bolt.Tx) error {
							v := "false"
							if a.Checked() {
								v = "true"
							}
							b := tx.Bucket([]byte("alerts"))
							return b.Put([]byte(alert), []byte(v))
						})
					}
				}
			}(alert)
		}
		return nil
	})
}

func decodedIcon(i string) ([]byte, error) {
	if len(icons[i].Decoded) < 1 {
		img, err := base64.StdEncoding.DecodeString(icons[i].Base64)
		if err != nil {
			return []byte(" "), fmt.Errorf("Failed to get icon: %s", i)
		}
		icons[i] = icon{
			Base64:  icons[i].Base64,
			Decoded: img,
		}
	}

	return icons[i].Decoded, nil
}

func setBg() {
	bg := lastBg.getBg()
	if showBg {
		systray.SetTitle(bg)
	}
	currentBg.SetTitle(bg)
	if showBg {
		currentBg.Hide()
	}
	icon := lastBg.getIcon()
	systray.SetIcon(icon)
}

func toggleShowCurrent(menuItem *systray.MenuItem, db *bolt.DB) {
	if menuItem.Checked() {
		menuItem.Uncheck()
		currentBg.Show()
		systray.SetTitle("")
		showBg = false
	} else {
		menuItem.Check()
		currentBg.Hide()
		showBg = true
		systray.SetTitle(lastBg.format())
	}
	db.Update(func(tx *bolt.Tx) error {
		v := "false"
		if menuItem.Checked() {
			v = "true"
		}
		b := tx.Bucket([]byte("keys"))
		return b.Put([]byte("showBg"), []byte(v))
	})
}
