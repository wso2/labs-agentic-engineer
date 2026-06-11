/**
 * Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
 *
 * WSO2 LLC. licenses this file to you under the Apache License,
 * Version 2.0 (the "License"); you may not use this file except
 * in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

import { useEffect, useRef, useState } from 'react';
import { Box } from '@wso2/oxygen-ui';

interface AnimatedBannerProps {
  show: boolean;
  children: React.ReactNode;
}

export function AnimatedBanner({ show, children }: AnimatedBannerProps) {
  const [rendered, setRendered] = useState(show);
  const [isVisible, setIsVisible] = useState(false);
  const exitTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);

  useEffect(() => {
    if (show) {
      clearTimeout(exitTimer.current);
      setRendered(true);
      const raf = requestAnimationFrame(() =>
        requestAnimationFrame(() => setIsVisible(true))
      );
      return () => cancelAnimationFrame(raf);
    } else {
      setIsVisible(false);
      exitTimer.current = setTimeout(() => setRendered(false), 280);
      return () => clearTimeout(exitTimer.current);
    }
  }, [show]);

  if (!rendered) return null;

  return (
    <Box
      sx={{
        opacity: isVisible ? 1 : 0,
        transform: isVisible ? 'translateY(0)' : 'translateY(-6px)',
        transition: 'opacity 0.25s ease, transform 0.25s ease',
      }}
    >
      {children}
    </Box>
  );
}
