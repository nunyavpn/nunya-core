#!/usr/bin/env node
// The core's version: which one comes next. Same rule as nunya/scripts/version.mjs; keep them in step.
//
//   node scripts/version.mjs next         prints the version the next release gets
//
// Every merge to main is a release (release.yml), so the number is worked out, not typed: from
// the newest `vX.Y.Z` tag, a merge that adds a feature (`feat:`) bumps the middle number and
// anything else the last. The first number is never bumped here. It moves only when a person tags
// a stable release by hand (`v1.0.0`), because "this is stable" is a decision, not a commit type.
//
// Tags are the source of truth, not the files: a release that failed to build leaves a tag and no
// release, and the next merge still moves on from it rather than reusing a number. The files follow
// (nunya-mobile) follow; the core keeps no version file, the tag is stamped into the binary.
//
// Node rather than a shell script so the rule is unit-tested (`version.test.mjs`) and runs the same
// on a Mac and a runner; it imports nothing outside Node.

import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";

/** The bump commit this workflow pushes back to main. Not a change of its own. */
export const RELEASE_COMMIT = /^chore\(release\):/;

/** `feat:`, `feat(scope):`, and anything marked breaking (`fix!:`), which in beta is a feature. */
const FEATURE = /^(feat(\([^)]*\))?!?|[a-z]+(\([^)]*\))?!):/;

const PLAIN = /^(\d+)\.(\d+)\.(\d+)$/;

/**
 * The version after `last`, given the subjects of the commits since it; null when there is nothing
 * to release (only the workflow's own bump commit, or nothing at all).
 */
export function nextVersion(last, subjects) {
  const m = PLAIN.exec(last);
  if (!m) throw new Error(`${last} is not X.Y.Z`);
  const changes = subjects.filter((s) => s.trim() && !RELEASE_COMMIT.test(s));
  if (!changes.length) return null;
  const [x, y, z] = m.slice(1).map(Number);
  return changes.some((s) => FEATURE.test(s)) ? `${x}.${y + 1}.0` : `${x}.${y}.${z + 1}`;
}

/** The highest plain `vX.Y.Z` tag; suffixed ones (`v1.0.0-rc.1`) are someone's experiment. */
export function newestTag(tags) {
  const key = (t) => PLAIN.exec(t.slice(1)).slice(1).map(Number);
  const plain = tags.filter((t) => t.startsWith("v") && PLAIN.test(t.slice(1)));
  plain.sort((a, b) => {
    const [p, q] = [key(a), key(b)];
    return p[0] - q[0] || p[1] - q[1] || p[2] - q[2];
  });
  return plain.at(-1) ?? null;
}

const root = fileURLToPath(new URL("..", import.meta.url));
const git = (...args) => execFileSync("git", args, { cwd: root, encoding: "utf8" }).trim();

function next() {
  const tag = newestTag(git("tag", "--list", "v*", "--merged", "HEAD").split("\n"));
  // No release yet: the first one is where the line starts.
  if (!tag) return "0.1.0";
  return nextVersion(tag.slice(1), git("log", "--format=%s", `${tag}..HEAD`).split("\n"));
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  if (process.argv[2] === "next") console.log(next() ?? "");
  else {
    console.error("usage: version.mjs next");
    process.exit(2);
  }
}
