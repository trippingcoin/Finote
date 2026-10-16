package main

import (
	"finote/config"
	"finote/utils/env"
	"fmt"
)

func main() {
	var cfg config.Config

	if err := env.Load(&cfg); err != nil {
		fmt.Errorf("load config: %w", err)
	}

}
