// SPDX-License-Identifier: Apache-2.0
//go:build e2e

package test_driver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

func waitTimeout(wg *sync.WaitGroup, timeout time.Duration) bool {
	c := make(chan struct{})
	go func() {
		defer close(c)
		wg.Wait()
	}()
	select {
	case <-c:
		return false // completed normally
	case <-time.After(timeout):
		return true // timed out
	}
}

func shutdownBrokerConnection(t *testing.T, client mqtt.Client, _ *sync.WaitGroup) {
	defer client.Disconnect(0)

	t.Log("unplugging...")

	// Use a dedicated WaitGroup and subscription for the unplug sequence
	// to avoid races with the main test's WaitGroup which may have already
	// received the Idle state from the charging session ending naturally.
	var unplugged sync.WaitGroup
	unplugged.Add(1)
	once := sync.Once{}

	tok := client.Subscribe("everest_external/nodered/1/state/state_string", 0, func(_ mqtt.Client, msg mqtt.Message) {
		if string(msg.Payload()) == "Idle" {
			once.Do(func() { unplugged.Done() })
		}
	})
	if tok.WaitTimeout(1 * time.Second); tok.Error() != nil {
		t.Errorf("failed to subscribe for unplug: %v", tok.Error())
		return
	}

	tok = client.Publish("everest_external/nodered/1/carsim/cmd/modify_charging_session", 0, false, "unplug")
	if tok.WaitTimeout(1 * time.Second); tok.Error() != nil {
		t.Errorf("failed to publish unplug to topic: %v", tok.Error())
	}
	timedOut := waitTimeout(&unplugged, 10*time.Second)
	if timedOut {
		t.Logf("timed out waiting for unplug to complete (non-fatal)")
	}
}

func setupBrokerConnection(t *testing.T, wg *sync.WaitGroup) (mqtt.Client, func()) {
	brokerAddr := os.Getenv("MQTT_BROKER_ADDR")
	if brokerAddr == "" {
		brokerAddr = "127.0.0.1:1884"
	}

	opts := mqtt.NewClientOptions()
	opts.AddBroker(brokerAddr)

	client := mqtt.NewClient(opts)
	if token := client.Connect(); token.WaitTimeout(2*time.Second) && token.Error() != nil {
		t.Fatalf("failed to connect to MQTT broker: %v", token.Error())
	}

	return client, func() {
		shutdownBrokerConnection(t, client, wg)
	}
}

func TestRFIDCharge(t *testing.T) {
	wg := sync.WaitGroup{}

	client, shutdown := setupBrokerConnection(t, &wg)
	defer shutdown()

	state := "Unset"

	tok := client.Subscribe("everest_external/nodered/1/state/state_string", 0, func(client mqtt.Client, msg mqtt.Message) {
		t.Logf("charger status changed to %s\n", msg.Payload())

		switch string(msg.Payload()) {
		case "Wait for Auth":
			state = string(msg.Payload())
			wg.Done()
		case "Charging":
			state = string(msg.Payload())
			wg.Done()
		case "Idle":
			state = string(msg.Payload())
			wg.Done()
		}
	})
	if tok.WaitTimeout(1 * time.Second); tok.Error() != nil {
		t.Fatalf("failed to subscribe to topic: %v", tok.Error())
	}

	wg.Add(1)
	t.Logf("plug in...")
	tok = client.Publish("everest_external/nodered/1/carsim/cmd/execute_charging_session", 0, false,
		"sleep 1;iec_wait_pwr_ready;sleep 1;draw_power_regulated 16,3;sleep 5")

	if tok.WaitTimeout(1 * time.Second); tok.Error() != nil {
		t.Fatalf("failed to publish start charge to topic: %v", tok.Error())
	}

	timedOut := waitTimeout(&wg, 5*time.Second)
	if timedOut {
		t.Errorf("timed out waiting for ready to authorise")
	}
	if state != "Wait for Auth" {
		t.Fatalf("expected state to be Wait for Auth, got %s", state)
	}

	wg.Add(1)
	t.Logf("authorise charge...")
	tok = client.Publish("everest_api/dummy_token_provider/cmd/provide", 0, false,
		fmt.Sprintln("{\"id_token\":\"DEADBEEF\",\"authorization_type\":\"RFID\",\"prevalidated\":false,\"connectors\":[1]}"))
	if tok.WaitTimeout(1 * time.Second); tok.Error() != nil {
		t.Fatalf("failed to publish authorise to topic: %v", tok.Error())
	}

	timedOut = waitTimeout(&wg, 5*time.Second)
	if timedOut {
		t.Errorf("timed out waiting for authorise to complete")
	}
	if state != "Charging" {
		t.Fatalf("expected state to be Charging, got %s", state)
	}
}

func TestISO15118Charge(t *testing.T) {
	t.Skip("skipping test in short mode.")
	wg := sync.WaitGroup{}

	client, shutdown := setupBrokerConnection(t, &wg)
	defer shutdown()

	state := "Unset"

	tok := client.Subscribe("everest_external/nodered/1/state/state_string", 0, func(client mqtt.Client, msg mqtt.Message) {
		t.Logf("charger status changed to %s\n", msg.Payload())

		switch string(msg.Payload()) {
		case "PrepareCharging":
			state = string(msg.Payload())
			wg.Done()
		case "Idle":
			state = string(msg.Payload())
			wg.Done()
		}
	})
	if tok.WaitTimeout(1 * time.Second); tok.Error() != nil {
		t.Fatalf("failed to subscribe to topic: %v", tok.Error())
	}

	wg.Add(1)
	t.Logf("plug in...")
	tok = client.Publish("everest_external/nodered/1/carsim/cmd/execute_charging_session", 0, false,
		"sleep 1;iso_wait_slac_matched;iso_start_v2g_session Contract,AC_three_phase_core;iso_wait_pwr_ready;iso_draw_power_regulated 16,3;sleep 36000")
	if tok.WaitTimeout(1 * time.Second); tok.Error() != nil {
		t.Fatalf("failed to publish start charge to topic: %v", tok.Error())
	}

	timedOut := waitTimeout(&wg, 15*time.Second)
	if timedOut {
		t.Errorf("timed out waiting for charge to start")
	}
	if state != "PrepareCharging" {
		t.Fatalf("expected state to be PrepareCharging, got %s", state)
	}
}

// command is the part of a command of the manager API that the tests read.
type command struct {
	Id       string `json:"id"`
	Status   string `json:"status"`
	Progress *struct {
		State             string `json:"state"`
		TransactionId     string `json:"transactionId"`
		OcppTransactionId *int   `json:"ocppTransactionId"`
		StopReason        string `json:"stopReason"`
	} `json:"progress"`
}

// managerApi returns the base URL of the API of the manager, which the e2e setup runs in
// "dev" auth mode (no token needed).
func managerApi() string {
	addr := os.Getenv("MANAGER_API_ADDR")
	if addr == "" {
		addr = "127.0.0.1:9410"
	}
	return "http://" + addr + "/api/v0"
}

// postCommand posts a request of a command endpoint and returns the queued command.
func postCommand(t *testing.T, path string, body any) command {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal %s: %v", path, err)
	}
	resp, err := http.Post(managerApi()+path, "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer resp.Body.Close()
	answer, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("POST %s: status %d: %s", path, resp.StatusCode, answer)
	}
	var cmd command
	if err := json.Unmarshal(answer, &cmd); err != nil || cmd.Id == "" {
		t.Fatalf("POST %s: not a command: %s", path, answer)
	}
	return cmd
}

// waitForProgress polls a command until its progress has the state.
func waitForProgress(t *testing.T, id, state string, timeout time.Duration) command {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last command
	for time.Now().Before(deadline) {
		resp, err := http.Get(managerApi() + "/commands/" + id)
		if err == nil {
			answer, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK && json.Unmarshal(answer, &last) == nil &&
				last.Progress != nil && last.Progress.State == state {
				return last
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("command %s did not reach progress %s within %s: %+v", id, state, timeout, last)
	return last
}

// waitForState waits until the charger reports the state, or (with leave) any other
// state, on the channel of its state changes.
func waitForState(t *testing.T, states <-chan string, want string, leave bool, timeout time.Duration) string {
	t.Helper()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case state := <-states:
			if (state == want) != leave {
				return state
			}
		case <-timer.C:
			if leave {
				t.Fatalf("timed out waiting for the charger to leave %s", want)
			}
			t.Fatalf("timed out waiting for the charger to be %s", want)
			return ""
		}
	}
}

// A remote start through the API starts charging, and its command shows the transaction
// it began; a remote stop of that transaction ends it, and its command shows the stop.
func TestRemoteStartStop(t *testing.T) {
	wg := sync.WaitGroup{}
	client, shutdown := setupBrokerConnection(t, &wg)
	defer shutdown()

	states := make(chan string, 32)
	tok := client.Subscribe("everest_external/nodered/1/state/state_string", 0, func(_ mqtt.Client, msg mqtt.Message) {
		t.Logf("charger status changed to %s\n", msg.Payload())
		select {
		case states <- string(msg.Payload()):
		default:
		}
	})
	if tok.WaitTimeout(1 * time.Second); tok.Error() != nil {
		t.Fatalf("failed to subscribe to topic: %v", tok.Error())
	}

	t.Logf("plug in...")
	tok = client.Publish("everest_external/nodered/1/carsim/cmd/execute_charging_session", 0, false,
		"sleep 1;iec_wait_pwr_ready;sleep 1;draw_power_regulated 16,3;sleep 60")
	if tok.WaitTimeout(1 * time.Second); tok.Error() != nil {
		t.Fatalf("failed to publish start charge to topic: %v", tok.Error())
	}
	waitForState(t, states, "Wait for Auth", false, 5*time.Second)

	t.Logf("remote start...")
	start := postCommand(t, "/cs/cs001/start-transaction", map[string]any{"idTag": "DEADBEEF", "connectorId": 1})
	waitForState(t, states, "Charging", false, 10*time.Second)
	started := waitForProgress(t, start.Id, "transaction_started", 10*time.Second)
	if started.Progress.TransactionId == "" || started.Progress.OcppTransactionId == nil {
		t.Fatalf("the remote start does not show its transaction: %+v", started.Progress)
	}
	t.Logf("transaction %s (%d) started", started.Progress.TransactionId, *started.Progress.OcppTransactionId)

	t.Logf("remote stop...")
	stop := postCommand(t, "/cs/cs001/stop-transaction", map[string]any{"transactionId": started.Progress.TransactionId})
	waitForState(t, states, "Charging", true, 10*time.Second)
	stopped := waitForProgress(t, stop.Id, "transaction_stopped", 10*time.Second)
	if stopped.Progress.OcppTransactionId == nil || *stopped.Progress.OcppTransactionId != *started.Progress.OcppTransactionId {
		t.Fatalf("the remote stop shows another transaction: %+v", stopped.Progress)
	}
	t.Logf("transaction %s stopped: %s", strconv.Itoa(*stopped.Progress.OcppTransactionId), stopped.Progress.StopReason)
}
