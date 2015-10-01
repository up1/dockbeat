package main

import (
	"time"

	"github.com/elastic/libbeat/beat"
	"github.com/elastic/libbeat/cfgfile"
	"github.com/elastic/libbeat/logp"
	"github.com/elastic/libbeat/publisher"
	"github.com/fsouza/go-dockerclient"
	"github.com/elastic/libbeat/common"
)

type Dockerbeat struct {
	isAlive        bool
	period         time.Duration
	socket         string
	TbConfig       ConfigSettings
	dockerClient   *docker.Client
	events         publisher.Client
	eventGenerator EventGenerator
}

func (d *Dockerbeat) Config(b *beat.Beat) error {

	err := cfgfile.Read(&d.TbConfig, "")
	if err != nil {
		logp.Err("Error reading configuration file: %v", err)
		return err
	}

	if d.TbConfig.Input.Period != nil {
		d.period = time.Duration(*d.TbConfig.Input.Period) * time.Second
	} else {
		d.period = 1 * time.Second
	}
	if d.TbConfig.Input.Socket != nil {
		d.socket = *d.TbConfig.Input.Socket
	} else {
		d.socket = "unix:///var/run/docker.sock" // default docker socket location
	}

	logp.Debug("dockerbeat", "Init dockerbeat")
	logp.Debug("dockerbeat", "Follow docker socket %q\n", d.socket)
	logp.Debug("dockerbeat", "Period %v\n", d.period)

	return nil
}

func (d *Dockerbeat) Setup(b *beat.Beat) error {
	d.events = b.Events
	d.dockerClient, _ = docker.NewClient(d.socket)
	d.eventGenerator = EventGenerator{map[string]NetworkData{}}
	return nil
}

func (d *Dockerbeat) Run(b *beat.Beat) error {

	d.isAlive = true

	var err error

	for d.isAlive {
		time.Sleep(d.period)
		containers, err := d.dockerClient.ListContainers(docker.ListContainersOptions{})

		if err == nil {
			for _, container := range containers {
				d.exportContainerStats(container)
			}
		} else {
			logp.Err("Cannot get container list: %d", err)
		}

		d.eventGenerator.cleanOldStats(containers)
	}

	return err
}

func (d *Dockerbeat) Cleanup(b *beat.Beat) error {
	return nil
}

func (d *Dockerbeat) Stop() {
	d.isAlive = false
}

func (d *Dockerbeat) exportContainerStats(container docker.APIContainers) error {
	statsC := make(chan *docker.Stats)
	done := make(chan bool)
	errC := make(chan error, 1)
	statsOptions := docker.StatsOptions{container.ID, statsC, false, done, -1}
	go func() {
		errC <- d.dockerClient.Stats(statsOptions)
		close(errC)
	}()

	go func() {
		stats := <-statsC

		events := []common.MapStr{
			d.eventGenerator.getContainerEvent(&container, stats),
			d.eventGenerator.getCpuEvent(&container, stats),
			d.eventGenerator.getMemoryEvent(&container, stats),
			d.eventGenerator.getNetworkEvent(&container, stats),
		}

		d.events.PublishEvents(events)
	}()

	return nil
}