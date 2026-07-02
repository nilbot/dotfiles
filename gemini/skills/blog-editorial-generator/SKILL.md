---
name: blog-editorial-generator
description: >-
  Generates beautiful, premium A4-editorial styled HTML blog posts with TOML front matter for the Hugo blog at nilbot.net based on conversation context, logs, or files. Automatically resolves URL slug conflicts.
---

# Blog Editorial Generator

## Overview
This skill allows the agent to automatically generate and publish premium-looking, A4-styled newsletter and project report posts for your Hugo blog at `/Users/nilbot/devel/nilbot.net/blog`. 

It reads an active conversation context, log files, or specific documents, then formats them using the design system of the `dicer-go` newsletter template—complete with a parchment background, elegant Charter serifs, dot-leader Table of Contents, numeric grids, status badges, and styled metrics dashboards.

## Dependencies
* None. This is an **instruction-only skill** relying on core filesystem tools:
  * `list_dir` for directory indexing and collision prevention.
  * `view_file` for reading templates and source files.
  * `write_to_file` for writing the finalized post.

## Quick Start
To trigger this skill, the user can invoke the agent with manual commands like:
* *"write the walkthrough.md doc in my blog"*
* *"write the latest findings in our conversation in my blog"*
* *"draft an editorial post about our recent MLX architecture improvements"*

---

## Workflow

### Step 1: Detect Manual Trigger & Parse Inputs
1. Identify the user request and extract the target source content:
   * **File Input**: If the trigger mentions a file (e.g. `walkthrough.md`, `task.md`), locate and read the target file.
   * **Context Input**: If the trigger mentions "latest findings" or "conversation content", summarize the current active conversation session's core technical achievements, code solutions, benchmarks, and architectures.
2. Verify that the blog directory `/Users/nilbot/devel/nilbot.net/blog` exists.

### Step 3: Ingest the Styling Template
1. Read the default template at `/Users/nilbot/devel/nilbot.net/blog/templates/editorial_newsletter_template.md`.
2. Extract the premium CSS styles and the structural layout rules.

### Step 4: Write and Assemble the Editorial Layout
Draft a highly polished, content-rich blog post. You **must not omit** any major template components. The post must strictly incorporate:

1. **TOML Front Matter**:
   Enclosed in `+++` at the very top:
   ```toml
   title = "Project Name · Project Update · Subtitle/Topic"
   date = YYYY-MM-DDTHH:MM:SS+01:00  # Set to current local time or slightly in the past. Never set to a future time.
   description = "Engaging 1-2 sentence summary."
   tags = ["ProjectName", "Go", "MLX", "Reinforcement Learning"]
   ```
2. **Scoping Container & Scoped CSS**:
   Wrap everything in a unique container class:
   ```html
   <div class="editorial-newsletter-[slug]">
     <style>
       /* All CSS selectors MUST be prefixed with the outer container class to prevent leakage! */
       .editorial-newsletter-[slug] { ... }
       .editorial-newsletter-[slug] .cover { ... }
       .editorial-newsletter-[slug] h1 { ... }
       .editorial-newsletter-[slug] .metric-card { ... }
     </style>
     ...
   </div>
   ```
3. **Cover Section** (`<section class="cover">`):
   Standard cover with eyebrows, big title, subtitle hook, brand rule, tags, versioning details, and repository metadata.
4. **Table of Contents** (`<section class="toc">`):
   Dot-leader chapter items with interactive hover transitions.
5. **Numbered Chapters** (`<section class="chapter">`):
   Numbered titles (`01 · Chapter Name`), subheadings, highly readable lead paragraphs, and `.hl` brand highlighted terms.
6. **Metrics Grid** (`<div class="metrics">`):
   A multi-card metrics grid showing at least 3-4 quantitative wins (latency, throughput, or validation success rates) tailored to the source content.
7. **Key Takeaways Box** (`<div class="takeaway">`):
   A neat block summarizing conclusions or next steps.
8. **Striped Table & Badges** (`<table class="striped">`):
   A technical state table with status badges (`badge-done`, `badge-stub`, `badge-deferred`).
9. **Monospace Code blocks** (`<pre><code>`):
   Beautifully highlighted code config, build commands, or architectural flows.

### Step 5: Save & Verify Publication
1. Write the completed HTML file containing the front matter and raw HTML to `/Users/nilbot/devel/nilbot.net/blog/content/post/[non-colliding-slug].html`.
2. Report success to the user, highlighting:
   * The generated post title and non-colliding slug.
   * The path of the written file.
   * A summary of the metrics and takeaways included in the post.

---

## Common Mistakes

1. **Setting Future Dates in TOML**:
   If the `date` is set to a future time, Hugo will treat it as a scheduled post and will NOT render it during builds. Always use the current local time or a past time.
2. **Unscoped CSS Selectors**:
   Forgetting to prefix a style rule (e.g. writing `h2 { color: red; }` instead of `.editorial-newsletter-[slug] h2 { color: red; }`) will leak styles and break the global blog theme. Always scope every rule strictly.
3. **Outer HTML Boilerplate**:
   Including standard `<html>`, `<head>`, or `<body>` tags will break the Hugo page builder. Generate only the TOML front matter followed directly by the `<div class="editorial-newsletter-[slug]">` container.
