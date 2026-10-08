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
import { createLink } from "@tanstack/react-router";
import { Alert, Box, Button, Dialog, DialogActions, DialogContent, DialogContentText, DialogTitle, Link, Typography } from "@wso2/oxygen-ui";
import { Upload } from "@wso2/oxygen-ui-icons-react";
import { useImportSkill } from "../api/skills";

const ButtonLink = createLink(Button);

/**
 * The Import Panel: an AgentSkills tarball (a gzip-compressed SKILL.md and its
 * references) goes into the org skills repo as an imported skill. The
 * platform checks it and says what it took, with any warnings; the skill then
 * opens in its card.
 */
export function ImportSkillPanel({ repoUrl, onClose }: { repoUrl: string | undefined; onClose: () => void }) {
  const [file, setFile] = useState<File | null>(null);
  const importSkill = useImportSkill();
  const result = importSkill.data;

  return (
    <Dialog open onClose={onClose} maxWidth="sm" fullWidth>
      <DialogTitle>Import a skill</DialogTitle>
      <DialogContent sx={{ display: "flex", flexDirection: "column", gap: 2 }}>
        {result ? (
          <>
            <Alert severity="success">
              <strong>{result.name}</strong> is in your organization&apos;s skills
              {result.license ? `, under ${result.license}` : ""}.
            </Alert>
            {(result.warnings ?? []).length > 0 && (
              <Alert severity="warning">
                <Typography variant="body2" sx={{ fontWeight: 600, mb: 0.5 }}>
                  Imported with warnings
                </Typography>
                <Box component="ul" sx={{ m: 0, pl: 2.5 }}>
                  {(result.warnings ?? []).map((w) => (
                    <li key={w}>
                      <Typography variant="body2">{w}</Typography>
                    </li>
                  ))}
                </Box>
              </Alert>
            )}
          </>
        ) : (
          <>
            <DialogContentText>
              Choose an AgentSkills tarball (<code>.tar.gz</code>): a <code>SKILL.md</code> and the files beside it. The
              platform checks it and adds it to your organization&apos;s skills as an imported skill.
            </DialogContentText>
            <Box sx={{ display: "flex", alignItems: "center", gap: 1.5, flexWrap: "wrap" }}>
              <Button component="label" variant="outlined" startIcon={<Upload size={16} />}>
                Choose file
                <input
                  type="file"
                  accept=".tgz,.tar.gz,application/gzip"
                  hidden
                  onChange={(e) => {
                    importSkill.reset();
                    setFile(e.target.files?.[0] ?? null);
                  }}
                />
              </Button>
              {file && (
                <Typography variant="body2" color="text.secondary">
                  {file.name}
                </Typography>
              )}
            </Box>
            {repoUrl && (
              <Typography variant="caption" color="text.secondary">
                A skill can also arrive by pull request: merged into{" "}
                <Link href={repoUrl} target="_blank" rel="noreferrer">
                  the org skills repo
                </Link>
                , it shows here.
              </Typography>
            )}
            {importSkill.isError && <Alert severity="error">{importSkill.error.message}</Alert>}
          </>
        )}
      </DialogContent>
      <DialogActions>
        {result ? (
          <>
            <Button onClick={onClose}>Done</Button>
            <ButtonLink variant="contained" to="/skills/$name" params={{ name: result.name }} onClick={onClose}>
              Open it
            </ButtonLink>
          </>
        ) : (
          <>
            <Button onClick={onClose}>Cancel</Button>
            <Button
              variant="contained"
              disabled={!file || importSkill.isPending}
              onClick={() => file && importSkill.mutate(file)}
            >
              {importSkill.isPending ? "Importing…" : "Import"}
            </Button>
          </>
        )}
      </DialogActions>
    </Dialog>
  );
}
