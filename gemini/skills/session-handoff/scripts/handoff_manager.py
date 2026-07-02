#!/usr/bin/env python3
import argparse
import sys
import os
import re
import shutil
import hashlib
from pathlib import Path
from datetime import datetime

_MD_LINK_RE = re.compile(r'(!?)\[([^\]]*)\]\(\s*(<[^>]+>|[^)\s]+)((?:\s+"[^"]*")?)\s*\)')


def _url_to_fs_path(url: str):
    """Map a local file:// URL or absolute path to an absolute filesystem path.

    Returns None for web links, anchors, mailto, or already-relative links, which
    must be left untouched.
    """
    u = url.strip()
    if u.startswith("<") and u.endswith(">"):
        u = u[1:-1]
    if u.startswith("file://"):
        return os.path.abspath(os.path.expanduser(u[len("file://"):]))
    if u.startswith("~"):
        return os.path.abspath(os.path.expanduser(u))
    if os.path.isabs(u):
        return os.path.abspath(u)
    return None


def _is_within(path: str, root: str) -> bool:
    return path == root or path.startswith(root + os.sep)


def sanitize_links(text, repo_root, session_dir, archive_assets=True):
    """Rewrite absolute / file:// links so none resolves outside repo root.

    Antigravity brain artifacts frequently link with `file:///Users/.../brain/...`
    absolute URLs because the brain folder lives outside the repository. Such links
    break the moment the note is read from a fresh clone. The enforced invariant is
    **no link may resolve outside repo root** — so the only correct fix for an
    out-of-repo target is to bring it into the repo, never to compute a `../`
    chain that climbs out of repo root. (In-repo `../` links are fine and often
    required: a note may legitimately point up into `src/` or `.agents/`.)

      * Target inside the repo -> clone-safe relative link (from the note's dir).
        (Two paths under a common root always yield a relative path that stays
        within that root, so this never escapes.)
      * Any out-of-repo file    -> copy into `<note-dir>/assets/` and link to the
        copy (when archive_assets=True).
      * Anything that cannot be brought in (missing, a directory, or when
        archive_assets=False) -> demote to plain text.

    Returns (sanitized_text, stats) where stats counts rewritten/archived/demoted.
    """
    repo_root = os.path.abspath(str(repo_root))
    session_dir = os.path.abspath(str(session_dir))
    assets_dir = Path(session_dir) / "assets"
    stats = {"rewritten": 0, "archived": 0, "demoted": 0}

    def repl(m):
        bang, label, url, title = m.group(1), m.group(2), m.group(3), m.group(4)
        fs = _url_to_fs_path(url)
        if fs is None:
            return m.group(0)  # relative / web / anchor link — already clone-safe

        # In-repo target -> relative link, guaranteed not to escape repo root.
        if _is_within(fs, repo_root):
            rel = os.path.relpath(fs, session_dir)
            stats["rewritten"] += 1
            return f"{bang}[{label}]({rel}{title})"

        # Out-of-repo target: the only clone-safe fix is to bring it into the repo.
        if archive_assets and os.path.isfile(fs):
            assets_dir.mkdir(parents=True, exist_ok=True)
            digest = hashlib.sha1(fs.encode("utf-8")).hexdigest()[:8]
            dest = assets_dir / f"{digest}-{os.path.basename(fs)}"
            if not dest.exists():
                shutil.copy2(fs, dest)
            stats["archived"] += 1
            rel = os.path.relpath(dest, session_dir)
            return f"{bang}[{label}]({rel}{title})"

        # Cannot bring it in (missing, a directory, or archiving disabled) -> demote.
        stats["demoted"] += 1
        return f"{label} _(external, not in repo: `{fs}`)_"

    return _MD_LINK_RE.sub(repl, text), stats

def find_latest_session(workspace_dir: Path):
    sessions_dir = workspace_dir / "docs" / "sessions"
    if not sessions_dir.exists():
        print("No 'docs/sessions' directory found in workspace.")
        return None
    
    md_files = list(sessions_dir.glob("**/*.md"))
    if not md_files:
        print("No session handoffs found.")
        return None
    
    # Sort files by directory date (YYYYMMDD) and filename timestamp (HHMMSS)
    # E.g. docs/sessions/20260527/171149-some-title.md
    def get_sort_key(file_path: Path):
        relative = file_path.relative_to(sessions_dir)
        parts = relative.parts
        if len(parts) >= 2:
            date_str = parts[-2]  # e.g. '20260527'
            time_part = parts[-1].split("-")[0]  # e.g. '171149'
            try:
                return (int(date_str), int(time_part))
            except ValueError:
                pass
        # Fallback to file mtime if formatting doesn't match
        return (0, file_path.stat().st_mtime)

    md_files.sort(key=get_sort_key, reverse=True)
    return md_files[0]

def cmd_start(args):
    workspace = Path(args.workspace).resolve()
    print(f"Scanning workspace for previous handoff logs: {workspace}")
    latest = find_latest_session(workspace)
    if latest:
        print(f"\n[FOUND LATEST SESSION LOG] at {latest.relative_to(workspace)}")
        print("=" * 80)
        try:
            content = latest.read_text(encoding="utf-8")
            print(content)
        except Exception as e:
            print(f"Error reading file: {e}")
        print("=" * 80)
    else:
        print("\nNo previous session logs found. This may be a new project.")

def cmd_record(args):
    workspace = Path(args.workspace).resolve()
    app_data_dir = Path(args.app_data_dir).resolve()
    conv_id = args.conv_id.strip()
    
    brain_dir = app_data_dir / "brain" / conv_id
    print(f"Resolving brain directory: {brain_dir}")
    
    if not brain_dir.exists():
        print(f"Error: Brain directory for conversation ID {conv_id} not found at {brain_dir}.", file=sys.stderr)
        sys.exit(1)
        
    walkthrough_file = brain_dir / "walkthrough.md"
    if not walkthrough_file.exists():
        print(f"Error: Mandatory 'walkthrough.md' not found at {walkthrough_file}.", file=sys.stderr)
        print("Please ensure you have run the task and generated the walkthrough first.", file=sys.stderr)
        sys.exit(1)
        
    print("Found mandatory walkthrough.md")
    walkthrough_content = walkthrough_file.read_text(encoding="utf-8")
    
    # Auto-detect situational files
    plan_file = brain_dir / "implementation_plan.md"
    plan_content = ""
    if plan_file.exists() and (args.include_plan or args.auto_detect):
        print("Found situational implementation_plan.md")
        plan_content = plan_file.read_text(encoding="utf-8")
        
    task_file = brain_dir / "task.md"
    task_content = ""
    if task_file.exists() and (args.include_task or args.auto_detect):
        print("Found situational task.md")
        task_content = task_file.read_text(encoding="utf-8")
        
    # Generate filename and target directory
    now = datetime.now()
    date_folder = now.strftime("%Y%m%d")
    timestamp = now.strftime("%H%M%S")
    
    target_dir = workspace / "docs" / "sessions" / date_folder
    target_dir.mkdir(parents=True, exist_ok=True)
    
    filename = f"{timestamp}-{args.title}.md"
    output_path = target_dir / filename
    
    # Compile the final handoff document
    handoff_doc = []
    handoff_doc.append(f"# Session Handoff: {args.title.replace('-', ' ').title()}")
    handoff_doc.append("")
    handoff_doc.append(f"- **Date**: {now.strftime('%Y-%m-%d %H:%M:%S')}")
    handoff_doc.append(f"- **Conversation ID**: `{conv_id}`")
    handoff_doc.append("")
    handoff_doc.append("## 📌 Project Overview & Handoff Summary")
    handoff_doc.append("")
    handoff_doc.append("### Original User Request")
    handoff_doc.append(f"> {args.prompt}")
    handoff_doc.append("")
    
    if plan_content:
        handoff_doc.append("## 📋 Proposed Implementation Plan")
        handoff_doc.append("")
        # Remove top level header from imported file if it exists, to maintain hierarchy
        lines = plan_content.splitlines()
        if lines and lines[0].startswith("# "):
            lines = lines[1:]
        handoff_doc.append("\n".join(lines).strip())
        handoff_doc.append("")
        
    if task_content:
        handoff_doc.append("## 🎯 Tasks & Progress Tracking")
        handoff_doc.append("")
        lines = task_content.splitlines()
        if lines and lines[0].startswith("# "):
            lines = lines[1:]
        handoff_doc.append("\n".join(lines).strip())
        handoff_doc.append("")
        
    handoff_doc.append("## 🔍 Walkthrough & Verification")
    handoff_doc.append("")
    lines = walkthrough_content.splitlines()
    if lines and lines[0].startswith("# "):
        lines = lines[1:]
    handoff_doc.append("\n".join(lines).strip())
    handoff_doc.append("")
    
    final_text = "\n".join(handoff_doc)
    final_text, link_stats = sanitize_links(
        final_text,
        repo_root=workspace,
        session_dir=target_dir,
        archive_assets=args.archive_assets,
    )

    try:
        output_path.write_text(final_text, encoding="utf-8")
        print(f"\nSuccess! Chronological handoff logged successfully.")
        print(f"Handoff File: {output_path}")
        print(f"Relative Path: docs/sessions/{date_folder}/{filename}")
        print(
            "Link sanitation: "
            f"{link_stats['rewritten']} rewritten relative, "
            f"{link_stats['archived']} archived to assets/, "
            f"{link_stats['demoted']} demoted to text."
        )
    except Exception as e:
        print(f"Error writing handoff file: {e}", file=sys.stderr)
        sys.exit(1)

def main():
    parser = argparse.ArgumentParser(description="Session Handoff & Documentation Manager")
    subparsers = parser.add_subparsers(dest="command", required=True)
    
    # Start command
    p_start = subparsers.add_parser("start", help="Read the latest handoff from workspace")
    p_start.add_argument("--workspace", required=True, help="Path to the active workspace")
    p_start.set_defaults(func=cmd_start)
    
    # Record command
    p_record = subparsers.add_parser("record", help="Record and archive session artifacts")
    p_record.add_argument("--workspace", required=True, help="Path to the active workspace")
    p_record.add_argument("--conv-id", required=True, help="Current Antigravity conversation ID")
    p_record.add_argument("--title", required=True, help="Short, hyphenated title for the session")
    p_record.add_argument("--prompt", required=True, help="The original user request / prompt")
    p_record.add_argument("--app-data-dir", default="/Users/nilbot/.gemini/antigravity", help="Antigravity app data directory")
    p_record.add_argument("--include-plan", action="store_true", help="Force include implementation_plan.md")
    p_record.add_argument("--include-task", action="store_true", help="Force include task.md")
    p_record.add_argument("--no-auto", dest="auto_detect", action="store_false", help="Disable auto-detecting plan and task files")
    p_record.add_argument("--no-archive-assets", dest="archive_assets", action="store_false", help="Demote brain-internal artifact links to text instead of copying them into the note's assets/ folder")
    p_record.set_defaults(func=cmd_record, auto_detect=True, archive_assets=True)
    
    args = parser.parse_args()
    args.func(args)

if __name__ == "__main__":
    main()
