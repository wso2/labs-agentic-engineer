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

import { useState } from "react";
import {
  Box,
  Card,
  CardActionArea,
  CardContent,
  Grid,
  PageContent,
  Stack,
  Typography,
} from "@wso2/oxygen-ui";
import { CreditCard, Mail, MessageSquare } from "@wso2/oxygen-ui-icons-react";
import { Link, useNavigate } from "@tanstack/react-router";
import { PageHeader } from "../../../components/PageHeader";
import { PromptComposer } from "../../projects/components/PromptComposer";

const EXAMPLES = [
  {
    icon: <CreditCard size={24} />,
    title: "Stripe",
    prompt:
      "Register Stripe as a payments integration. Runtime config: API credentials and webhook signing.",
  },
  {
    icon: <Mail size={24} />,
    title: "SendGrid",
    prompt:
      "Register SendGrid for transactional email. Runtime config: API credentials and a default from-address.",
  },
  {
    icon: <MessageSquare size={24} />,
    title: "Twilio",
    prompt: "Register Twilio for SMS. Runtime config: account credentials.",
  },
] as const;

export function RegisterComposerPage() {
  const navigate = useNavigate();
  const [prompt, setPrompt] = useState("");
  const [files, setFiles] = useState<File[]>([]);

  const start = (text: string) => {
    const trimmed = text.trim();
    if (!trimmed) return;
    void navigate({
      to: "/resources/register/form",
      state: { registerPrompt: trimmed },
    });
  };

  return (
    <PageContent>
      <Box sx={{ maxWidth: 720, mx: "auto", pt: { xs: 4, md: 8 } }}>
        <Stack spacing={4}>
          <PageHeader
            title="Register an External resource"
            subtitle="Describe the External resource your organization's projects can connect to, and the runtime environment variables they need to configure."
            backTo={{
              link: <Link to="/resources" />,
              label: "Back to Resources",
            }}
          />
          <PromptComposer
            prompt={prompt}
            onPromptChange={setPrompt}
            files={files}
            onFilesChange={setFiles}
            onSubmit={() => start(prompt)}
          />
          <Grid container spacing={2}>
            {EXAMPLES.map((example) => (
              <Grid key={example.title} size={{ xs: 12, sm: 4 }}>
                <Card variant="outlined" sx={{ height: "100%" }}>
                  <CardActionArea
                    sx={{
                      height: "100%",
                      display: "flex",
                      flexDirection: "column",
                      justifyContent: "flex-start",
                      alignItems: "stretch",
                    }}
                    onClick={() => start(example.prompt)}
                  >
                    <CardContent>
                      <Stack
                        direction="row"
                        spacing={1.5}
                        sx={{ alignItems: "center", mb: 1 }}
                      >
                        {example.icon}
                        <Typography variant="subtitle2" noWrap>
                          {example.title}
                        </Typography>
                      </Stack>
                      <Typography variant="body2" color="text.secondary">
                        {example.prompt}
                      </Typography>
                    </CardContent>
                  </CardActionArea>
                </Card>
              </Grid>
            ))}
          </Grid>
        </Stack>
      </Box>
    </PageContent>
  );
}
