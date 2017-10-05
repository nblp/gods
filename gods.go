// This programm collects some system information, formats it nicely and sets
// the X root windows name so it can be displayed in the dwm status bar.
//
// For license information see the file LICENSE
//  TODO add name + type of network to wich i connect
package main

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	bpsSign   = " Bps"
	kibpsSign = "KBps"
	mibpsSign = "MBps"

	netLogoSign   = string('\u25CD')
	netDownUpSign = string('\u21F5')
	netWifiSign   = "[W]"
	netEthSign    = "[E]"
	netNoSign     = "[?]"

	batLogoSign   = string('\u26A1')
	unpluggedSign = "[ ]"
	pluggedSign   = "[" + string('\u26A1') + "]"

	cpuLogoSign = string('\u25A3')
	cpuSign     = string('\u27A4') + string('\u25A3')

	KBSign = "KB"
	MBSign = "MB"
	GBSign = "GB"

	memLogoSign = string('\u2338')
	memSign     = string('\u27A4') + string('\u2338')

	floatSeparator = "."
	dateSeparator  = " "
	fieldSeparator = "|"

	alertSign   = string('\u2620')
	warningSign = string('\u26A0')
	noneSign    = string('\u2756')
)

var (
	alarmStatus = map[string]string{
		netLogoSign: noneSign,
		batLogoSign: noneSign,
		cpuLogoSign: noneSign,
		memLogoSign: noneSign,
	}
	netDevs = map[string]struct{}{
		"enp0s25:": {},
		"wlp3s0:":  {},
	}
	cores = runtime.NumCPU() // count of cores to scale cpu usage
	rxOld = 0
	txOld = 0
)

// upgradeAlarmStatus modify the status of a service according to the current
// level and the one passed in argument. Keep the more severe level
func upgradeAlarmStatus(service string, level string) {
	levelNow, ok := alarmStatus[service]
	if ok {
		switch levelNow {
		case alertSign:
			alarmStatus[service] = alertSign
		case warningSign:
			if level == alertSign {
				alarmStatus[service] = alertSign
			}
		default:
			alarmStatus[service] = level
		}
	}
}

// updateAlarms read alarmStatus and generate a fancy alarms display.
func updateAlarms() string {
	var isOn bool = false
	pre := noneSign
	alert := alertSign + ":"
	warn := warningSign + ":"
	for key, val := range alarmStatus {
		if val == alertSign {
			isOn = true
			alert += key
		} else if val == warningSign {
			isOn = true
			warn += key
		}
	}
	if isOn {
		return pre + " " + alert + " " + warn
	} else {
		return pre
	}
}

// fixed builds a fixed width string with given pre- and fitting suffix
func fixed(rate int) string {
	if rate < 0 {
		return " ERR"
	}

	var decDigit = 0
	var unit = bpsSign // default: display as B/s

	switch {
	case rate >= (1000 * 1024 * 1024): // > 999 MiB/s
		upgradeAlarmStatus(netLogoSign, warningSign)
		return " ERR"
	case rate >= (1000 * 1024): // display as MiB/s
		decDigit = (rate / 1024 / 1024) % 10
		rate /= (1024 * 1024)
		unit = mibpsSign
	case rate >= 1000: // display as KiB/s
		decDigit = (rate / 1024) % 10
		rate /= 1024
		unit = kibpsSign
	}

	var formated = ""
	if rate >= 100 {
		formated = fmt.Sprintf(" %3d", rate)
	} else if rate >= 10 {
		formated = fmt.Sprintf("%2d.%1d", rate, decDigit)
	} else {
		formated = fmt.Sprintf("%1d.%2d", rate, decDigit)
	}
	return strings.Replace(formated, ".", floatSeparator, 1) + unit
}

// updateNetUse reads current transfer rates of certain network interfaces
func updateNetUse() string {
	file, err := os.Open("/proc/net/dev")
	if err != nil {
		return netDownUpSign + " ERR"
	}
	defer file.Close()

	var void = 0 // target for unused values
	var devName string
	var dev, rx, tx, rxNow, txNow = "", 0, 0, 0, 0
	var scanner = bufio.NewScanner(file)
	for scanner.Scan() {
		_, err = fmt.Sscanf(scanner.Text(), "%s %d %d %d %d %d %d %d %d %d",
			&dev, &rx, &void, &void, &void, &void, &void, &void, &void, &tx)
		if _, ok := netDevs[dev]; ok {
			rxNow += rx
			txNow += tx
			if rx > 0 || tx > 0 {
				switch dev {
				case "wlp3s0:":
					devName = netWifiSign
				case "enp0s25:":
					devName = netEthSign
				default:
					upgradeAlarmStatus(netLogoSign, warningSign)
					devName = netNoSign
				}
			}
		}
	}

	defer func() { rxOld, txOld = rxNow, txNow }()
	return fmt.Sprintf("%s%s%s%s", devName, fixed(rxNow-rxOld), netDownUpSign, fixed(txNow-txOld))
}

// updatePower reads the current battery and power plug status
func updatePower() string {
	const powerSupply = "/sys/class/power_supply/"
	var enFull, enNow, enPerc int = 0, 0, 0
	var plugged, err = ioutil.ReadFile(powerSupply + "AC/online")
	if err != nil {
		return "ERR"
	}
	batts, err := ioutil.ReadDir(powerSupply)
	if err != nil {
		return "ERR"
	}

	readval := func(name, field string) int {
		var path = powerSupply + name + "/"
		var file []byte
		if tmp, err := ioutil.ReadFile(path + "energy_" + field); err == nil {
			file = tmp
		} else if tmp, err := ioutil.ReadFile(path + "charge_" + field); err == nil {
			file = tmp
		} else {
			return 0
		}

		if ret, err := strconv.Atoi(strings.TrimSpace(string(file))); err == nil {
			return ret
		}
		return 0
	}

	for _, batt := range batts {
		name := batt.Name()
		if !strings.HasPrefix(name, "BAT") {
			continue
		}

		enFull += readval(name, "full")
		enNow += readval(name, "now")
	}

	if enFull == 0 { // Battery found but no readable full file.
		return "ERR"
	}

	enPerc = enNow * 100 / enFull
	var icon = unpluggedSign
	if string(plugged) == "1\n" {
		icon = pluggedSign
	} else {
		if enPerc < 10 {
			upgradeAlarmStatus(batLogoSign, alertSign)
		} else if enPerc < 15 {
			upgradeAlarmStatus(batLogoSign, warningSign)
		}
	}
	return fmt.Sprintf("%3d%s", enPerc, icon)
}

// updateCPUUse reads the last minute sysload and scales it to the core count
func updateCPUUse() string {
	var load float64
	var loadavg, err = ioutil.ReadFile("/proc/loadavg")
	if err != nil {
		return cpuSign + "ERR"
	}
	_, err = fmt.Sscanf(string(loadavg), "%f", &load)
	if err != nil {
		return cpuSign + "ERR"
	}
	if load > 0.75*float64(cores) {
		upgradeAlarmStatus(cpuLogoSign, warningSign)
	} else if load > 0.9*float64(cores) {
		upgradeAlarmStatus(cpuLogoSign, alertSign)
	}
	return fmt.Sprintf("%.2f%s", load, cpuSign)
}

// fixedMem take a value val in kiloByte and return its convertion in KB, MB
// or GB  as well as the sign chosen to represent the unit.
func fixedMem(val float64) (float64, string) {
	MB_limit := 1024.0
	GB_limit := 1024.0 * 1024.0

	switch {
	case val > GB_limit:
		return val / GB_limit, GBSign
	case val > MB_limit:
		return val / MB_limit, MBSign
	default:
		return val, KBSign
	}
}

// updateMemUse reads the memory used by applications and scales to [0, 100]
func updateMemUse() string {
	var file, err = os.Open("/proc/meminfo")
	if err != nil {
		return memSign + "ERR"
	}
	defer file.Close()

	// done must equal the flag combination (0001 | 0010 | 0100 | 1000) = 15
	var total, used, done = 0.0, 0.0, 0
	for info := bufio.NewScanner(file); done != 15 && info.Scan(); {
		var prop, val = "", 0.0
		if _, err = fmt.Sscanf(info.Text(), "%s %f", &prop, &val); err != nil {
			return memSign + "ERR"
		}
		switch prop {
		case "MemTotal:":
			total = val
			used += val
			done |= 1
		case "MemFree:":
			used -= val
			done |= 2
		case "Buffers:":
			used -= val
			done |= 4
		case "Cached:":
			used -= val
			done |= 8
		}
	}
	if float64(used) > 0.75*float64(total) {
		upgradeAlarmStatus(memLogoSign, warningSign)
	} else if float64(used) > 0.9*float64(total) {
		upgradeAlarmStatus(memLogoSign, alertSign)
	}
	u, uFormat := fixedMem(used)
	t, tFormat := fixedMem(total)
	return fmt.Sprintf("%.2f%s/%.2f%s%s", u, uFormat, t, tFormat, memSign)
}

// main updates the dwm statusbar every second
func main() {
	for {
		var status = []string{
			"",
			updateAlarms(),
			updateNetUse(),
			updateCPUUse(),
			updateMemUse(),
			time.Now().Local().Format("15:04:05" + dateSeparator + "Mon 02 Jan 2006"),
			updatePower(),
		}
		exec.Command("xsetroot", "-name", strings.Join(status, fieldSeparator)).Run()

		// sleep until beginning of next second
		var now = time.Now()
		time.Sleep(now.Truncate(time.Second).Add(time.Second).Sub(now))
	}
}
