// readAllCSS resolves @import directives in styles.css and returns the
// concatenated CSS content. This supports the split-file CSS layout where
// styles.css contains only @import statements pointing to per-concern files.

import { readFileSync } from "fs";
import { join, dirname } from "path";

export function readAllCSS(stylesPath) {
  const raw = readFileSync(stylesPath, "utf8");
  const dir = dirname(stylesPath);
  return raw.replace(/@import\s+"([^"]+)"\s*;/g, (_match, file) => {
    return readFileSync(join(dir, file), "utf8");
  });
}
