package main

import (
	"fmt"
	"log"
	"os"
	"runtime"
	"strings"
	"time"

	"flag"

	"github.com/mitchellh/go-ps"
	"github.com/shirou/gopsutil/net"
	pro "github.com/shirou/gopsutil/process"
)

func FindProcessByName(name string) (prs []ps.Process, err error) {
	var procs []ps.Process
	procs, err = ps.Processes()
	if err != nil {
		return
	}
	for _, proc := range procs {
		if strings.Contains(proc.Executable(), name) {
			prs = append(prs, proc)
		}
	}
	return
}

func MustIOCountersOfInterface(iface string) *net.IOCountersStat {
	stats, err := net.IOCounters(true)
	if err != nil {
		log.Fatalf("get stat of network interface %s failed: %v\n", iface, err)
	}
	for i, _ := range stats {
		if stats[i].Name == iface {
			return &stats[i]
		}
	}
	var names []string
	for _, ifa := range stats {
		names = append(names, ifa.Name)
	}
	log.Fatalf("network interface not found: %s, available=%s\n", iface, strings.Join(names, ","))
	return nil
}

func CalcNetSpeed(now, last *net.IOCountersStat, duration time.Duration) (in, out int64, err error) {
	if now.BytesRecv < last.BytesRecv || now.BytesSent < last.BytesSent {
		return -1, -1, fmt.Errorf("netstat expected to be increased, but actual is (now = %+v, last = %+v)", now, last)
	}
	return int64(now.BytesRecv-last.BytesRecv) / int64(duration/time.Second), int64(now.BytesSent-last.BytesSent) / int64(duration/time.Second), nil
}

var (
	flagPid      int
	flagOutput   string
	flagInterval time.Duration
	flagIface    string
	flagHeader   bool
)

func main() {
	runtime.GOMAXPROCS(2)
	flag.BoolVar(&flagHeader, "header", false, "if to print csv header")
	flag.IntVar(&flagPid, "pid", -1, "pid of process")
	flag.StringVar(&flagOutput, "output", "", "file path of result to write to")
	flag.StringVar(&flagIface, "iface", "eth0", "network interface to monitor. use ifconfig to view all interfaces.")
	flag.DurationVar(&flagInterval, "interval", time.Second*5, "capture interval, must provide time unit, such as 5s")
	flag.Parse()
	if flagPid == -1 {
		log.Fatalf("invalid pid %d\n", flagPid)
	}
	o, err := os.OpenFile(flagOutput, os.O_WRONLY|os.O_CREATE|os.O_APPEND|os.O_SYNC, 0666)
	if err != nil {
		log.Fatalf("open output file of path %s failed:%v\n", flagOutput, err)
	}
	defer o.Close()
	if flagHeader {
		o.WriteString("unixtimestamp,time,cpu,mem,threads,netin,netout\n")
	}
	p, err := pro.NewProcess(int32(flagPid))
	if err != nil {
		log.Fatalf("open process of pid %d failed: %v\n", flagPid, err)
	}
	lasttime := time.Now()
	lastnetstat := MustIOCountersOfInterface(flagIface)
	if err != nil {
		log.Fatalf("get stats of network interface %s failed: %v\n", flagIface, err)
	}
	var lastspeedin, lastspeedout int64
	for {
		time.Sleep(flagInterval)
		now := time.Now()
		cpu, err := p.Percent(0)
		if err != nil {
			log.Fatalf("get cpu percent failed: %v\n", err)
		}
		mem, err := p.MemoryInfo()
		if err != nil {
			log.Fatalf("get mem info failed: %v\n", err)
		}
		netstat := MustIOCountersOfInterface(flagIface)
		if err != nil {
			log.Fatalf("get stats of network interface %s failed: %v\n", flagIface, err)
		}

		speedin, speedout, err := CalcNetSpeed(netstat, lastnetstat, now.Sub(lasttime))
		if err != nil {
			log.Printf("calc net speed failed: %v\n", err)
			speedin, speedout = lastspeedin, lastspeedout
		}

		tc, err := p.NumThreads()
		if err != nil {
			log.Fatalf("get threads num failed:%v\n", err)
		}
		line := fmt.Sprintf("%d,%s,%.2f,%.3fMB,%d,%dKbps,%dKbps", now.Unix(), now.Format("20060102 15:04:05"), cpu,
			float64(mem.RSS)/1024/1024, tc, speedin*8/1024, speedout*8/1024)
		o.WriteString(line)
		o.WriteString("\n")
		lasttime = now
		lastnetstat = netstat
		lastspeedin, lastspeedout = speedin, speedout
	}
}
