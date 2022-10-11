package main

import (
	"antinat/config"
	"antinat/log"
	"antinat/protocols"
)

func main() {
	run()
	ch := make(chan int)
	<-ch
}

func run() {
	instances, _ := config.GetInstances()
	for _, v := range instances {
		go func(inst string) {
			for {
				cfg := config.NewConfig(inst)
				if inst, err := protocols.NewRunner(cfg); err != nil {
					log.Error("<%s> start error: %s", cfg.GetInstanceName(), err.Error())
				} else {
					inst.Run()
				}
			}
		}(v)
	}
}
