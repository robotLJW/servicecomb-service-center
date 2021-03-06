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
package plugin

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

type testPluginConfigurator struct {
}

func (c *testPluginConfigurator) GetImplName(_ Kind) string {
	return "test"
}
func (c *testPluginConfigurator) GetPluginDir() string {
	return "dir"
}

func TestRegisterConfigurator(t *testing.T) {
	t.Run("default configurator should not be nil", func(t *testing.T) {
		assert.NotNil(t, GetConfigurator())
	})
	t.Run("register a customize configurator", func(t *testing.T) {
		RegisterConfigurator(&testPluginConfigurator{})
		assert.Equal(t, "test", GetConfigurator().GetImplName(""))
		assert.Equal(t, "dir", GetConfigurator().GetPluginDir())
	})
}
