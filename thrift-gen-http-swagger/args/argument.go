/*
 * Copyright 2024 CloudWeGo Authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package args

import (
	"fmt"

	"github.com/hertz-contrib/swagger-generate/common/utils"
)

type Arguments struct {
	OutputDir string
}

func (a *Arguments) Unpack(args []string) error {
	err := utils.UnpackArgs(args, a)
	if err != nil {
		return fmt.Errorf("unpack argument failed: %s", err)
	}
	return nil
}
