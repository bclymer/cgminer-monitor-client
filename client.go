package main

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type MinerCommand struct {
	Command   string `json:"command"`
	Parameter string `json:"parameter"`
}

type Config struct {
	Interval       int           `json:"interval"`
	ParsedInterval time.Duration `json:"-"`
	ServerHost     string        `json:"serverHost"`
	ServerPort     string        `json:"serverPort"`
	MinerHost      string        `json:"minerHost"`
	MinerPort      string        `json:"minerPort"`
	DeviceName     string        `json:"deviceName"`
	ServerPassword string        `json:"serverPassword"`
}

type CgMinerStats struct {
	DeviceName string `json:"deviceName"`
	When       int64  `json:"when"`
	Status     []struct {
		When int64 `json:"When"`
	} `json:"STATUS"`
	Devs []struct {
		GPU               int     `json:"GPU"`
		Enabled           string  `json:"Enabled"`
		Status            string  `json:"Status"`
		Temperature       float64 `json:"Temperature"`
		FanSpeed          int     `json:"Fan Speed"`
		FanPercent        int     `json:"Fan Percent"`
		GpuClock          int     `json:"GPU Clock"`
		MemClock          int     `json:"Memory Clock"`
		GpuVoltage        float64 `json:"GPU Voltage"`
		GpuActivity       int     `json:"GPU Activity"`
		Powertune         int     `json:"Powertune"`
		MhsAv             float64 `json:"MHS av"`
		MhsFiveSeconds    float64 `json:"MHS 5s"`
		Accepted          int     `json:"Accepted"`
		Rejected          int     `json:"Rejected"`
		HardwareErrors    int     `json:"Hardware Errors"`
		Utility           float64 `json:"Utility"`
		Intensity         string  `json:"Intensity"`
		LastSharePool     int64   `json:"Last Share Pool"`
		LastShareTime     int64   `json:"Last Share Time"`
		TotalMh           float64 `json:"Total MH"`
		DiffOneWork       int64   `json:"Diff1 Work"`
		DiffAccepted      float64 `json:"Difficulty Accepted"`
		DiffRejected      float64 `json:"Difficulty Rejected"`
		LastShareDiff     float64 `json:"Last Share Difficulty"`
		LastValidWorkd    int64   `json:"Last Valid Work"`
		DeviceHardwarePct float64 `json:"Device Hardware%"`
		DeviceRejectedPct float64 `json:"Device Rejected%"`
		DeviceElapsed     int64   `json:"Device Elapsed"`
	} `json:"DEVS"`
}

var (
	uploadQueue = make(chan (os.FileInfo))
	config      Config
)

func main() {
	loadConfig()
	os.Mkdir("./stats", 7777)
	go uploadStatsOnFs()
	go uploadStatQueue()
	for {
		//var i int
		//_, err := fmt.Scanf("%d", &i)

		response, err := queryMiner("devs", "")
		if err != nil {
			continue
		}
		var devs CgMinerStats
		err = json.Unmarshal([]byte(response), &devs)
		if err != nil {
			fmt.Println("Parse Error:", err)
			continue
		} else {
			//fmt.Println("Response:", strings.TrimRight(string(response), "\x00"))
		}
		//go uploadStat(devs)
		go writeCgMinerStats(devs)
		time.Sleep(config.ParsedInterval * time.Second)
	}
}

func queryMiner(command, param string) (string, error) {
	commandDto := MinerCommand{
		Command:   command,
		Parameter: param,
	}
	commandBytes, err := json.Marshal(commandDto)
	if err != nil {
		fmt.Println("Marshal Error:", err)
		return "", err
	}
	conn, err := net.Dial("tcp", config.MinerHost+":"+config.MinerPort)
	if err != nil {
		fmt.Println("Dail Error:", err)
		return "", err
	}
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	_, err = conn.Write(commandBytes)
	if err != nil {
		fmt.Println("Write Error:", err)
		return "", err
	}
	response := make([]byte, 4096, 4096)
	conn.Read(response)
	if err != nil {
		fmt.Println("Read Error:", err)
		return "", err
	}
	return strings.TrimRight(string(response), "\x00"), nil
}

func loadConfig() {
	content, err := ioutil.ReadFile("config.json")
	if err != nil {
		panic(err)
	}
	err = json.Unmarshal(content, &config)
	if err != nil {
		panic(err)
	}
	config.ParsedInterval = time.Duration(config.Interval)
	fmt.Println("Server is: " + config.ServerHost + ":" + config.ServerPort)
	fmt.Println("Miner is: " + config.MinerHost + ":" + config.MinerPort)
	fmt.Println("Querying every", config.Interval, "seconds")
}

func writeCgMinerStats(minerStats CgMinerStats) {
	minerStats.DeviceName = config.DeviceName
	minerStats.When = minerStats.Status[0].When
	stats, err := json.Marshal(minerStats)
	if err != nil {
		fmt.Println("Failed marshaling minerStats", err)
		return
	}
	err = ioutil.WriteFile("stats/"+getStatsName(minerStats), stats, 0644)
	if err != nil {
		fmt.Println("Failed writing the file", err)
		return
	}
	uploadStatsOnFs()
}

func getStatsName(stats CgMinerStats) string {
	return config.DeviceName + "_" + strconv.FormatInt(stats.When, 10)
}

func uploadStat(devs CgMinerStats) {
	content, err := json.Marshal(devs)
	if err != nil {
		fmt.Println("F, no idea:", err)
		return
	}
	err = postStatString(content, getStatsName(devs))
	if err != nil {
		fmt.Println("Couldn't upload without write:", err)
		writeCgMinerStats(devs)
	} else {
		fmt.Println("Uploaded without writing to disk. Yay")
	}
}

func uploadStatsOnFs() {
	fmt.Println("Scanning for files to upload")
	files, err := ioutil.ReadDir("./stats/")
	if err != nil {
		fmt.Println("Failed reading ./stats/ dir", err)
	}
	fmt.Println("Found", len(files), "files to upload")
	for _, file := range files {
		uploadQueue <- file
	}
}

func uploadStatQueue() {
	for {
		select {
		case file := <-uploadQueue:
			fmt.Println("Uploading", file.Name())
			if err := postStatFile(file); err != nil {
				fmt.Println("Failed to upload file", err)
			} else {
				err := os.Remove("./stats/" + file.Name())
				if err != nil {
					fmt.Println("Failed deleting", file.Name(), err)
				} else {
					fmt.Println("Deleted", file.Name(), "after successful upload")
				}
			}
			break
		}
	}
}

func postStatFile(file os.FileInfo) error {
	extraParams := map[string]string{
		"name": file.Name(),
	}
	request, err := newfileUploadRequest("http://"+config.ServerHost+":"+config.ServerPort+"/stats", extraParams, "file", "./stats/"+file.Name())
	if err != nil {
		return err
	}
	return postRequest(request)
}

func postStatString(body []byte, fileName string) error {
	extraParams := map[string]string{
		"name": fileName,
	}
	request, err := newStringBodyRequest("http://"+config.ServerHost+":"+config.ServerPort+"/stats", extraParams, "file", body)
	if err != nil {
		return err
	}
	return postRequest(request)
}

func postRequest(request *http.Request) error {
	client := &http.Client{}
	resp, err := client.Do(request)
	if err != nil {
		return err
	} else {
		body := &bytes.Buffer{}
		_, err := body.ReadFrom(resp.Body)
		if err != nil {
			return err
		}
		resp.Body.Close()
		if resp.StatusCode != 201 {
			fmt.Println("Server said:", body.String())
			return errors.New("Status code returned was: " + strconv.Itoa(resp.StatusCode))
		}
	}
	return nil
}

// Creates a new file upload http request with optional extra params
func newfileUploadRequest(uri string, params map[string]string, paramName, path string) (*http.Request, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile(paramName, filepath.Base(path))
	if err != nil {
		return nil, err
	}
	_, err = io.Copy(part, file)

	for key, val := range params {
		_ = writer.WriteField(key, val)
	}
	err = writer.Close()
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", uri, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Server-Password", config.ServerPassword)
	return req, nil
}

// Creates a new file upload http request with optional extra params
func newStringBodyRequest(uri string, params map[string]string, paramName string, body []byte) (*http.Request, error) {
	req, err := http.NewRequest("POST", uri, bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "multipart/form-data; boundary="+randString(60))
	req.Header.Set("Server-Password", config.ServerPassword)
	return req, nil
}

func randString(n int) string {
	const alphanum = "0123456789abcdef"
	var bytes = make([]byte, n)
	rand.Read(bytes)
	for i, b := range bytes {
		bytes[i] = alphanum[b%byte(len(alphanum))]
	}
	return string(bytes)
}
