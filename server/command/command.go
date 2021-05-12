/*
 * Licensed to the Apache Software Foundation (ASF) under one or more
 * contributor license agreements.  See the NOTICE file distributed with
 * this work for additional information regarding copyright ownership.
 * The ASF licenses this file to You under the Apache License, Version 2.0
 * (the "License"); you may not use this file except in compliance with
 * the License.  You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package command

import (
	"github.com/urfave/cli"

	"github.com/apache/servicecomb-service-center/server/config"
	"github.com/apache/servicecomb-service-center/version"
)

// ParseConfig from cli
func ParseConfig(args []string) (err error) {
	app := cli.NewApp()
	app.Version = version.VERSION
	app.Usage = "servicecomb service center cmd line."
	app.Name = "servicecomb service center"
	app.Flags = []cli.Flag{
		cli.BoolFlag{
			Name:        "enable-rbac",
			Usage:       "enable rbac, example: --enable-rbac",
			Destination: &config.Server.Config.EnableRBAC,
		},
	}
	app.Action = func(c *cli.Context) error {
		return nil
	}

	err = app.Run(args)
	return
}
