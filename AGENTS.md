# Jotes Project Charter

## Purpose
Jotes is a note-taking web app designed to feel at home on both desktop and mobile. The product should stay focused on clarity, speed, and reliability, with a simple modern presentation.

## Core Direction
- Keep the experience straightforward, modern, and visually calm.
- Do not use emojis anywhere in the product.
- Prefer a filesystem-first model over database-driven storage.
- Keep deployment self-contained and easy to run.
- Target Linux only; cross-platform compatibility is out of scope unless explicitly requested.
- Treat mobile and desktop as first-class experiences.

## Dependencies Policy
**No CDN dependencies are allowed.** All fonts, libraries, and assets must be bundled locally within the project. This ensures:
- Complete offline functionality
- No external network requests for core functionality  
- Predictable performance without third-party DNS lookups
- Full control over asset versions and integrity

## Product Requirements
- The application is written in Go.
- Notes are stored on the server as plaintext files, not in a database.
- All text-based file formats should be supported, including `.txt`, `.json`, `.yaml`, `.yml`, `.toml`, `.html`, `.org`, `.md`, and similar plaintext formats.
- The app must detect newly added files without requiring a server restart.
- The app must perform a configurable full refresh and backup sync every 5 minutes by default so the presented state stays accurate.

## Routing and URL Model
- The server watches a user-configured root directory.
- HTTP paths should mirror the filesystem relative to that root.
- Example: if the configured root contains `test/test.md`, it should be available at `/test/test.md` on the running site.
- All static assets and application-specific endpoints must live under `/jotes`.

## Editing Experience
- Jotes includes a built-in editor.
- While editing, users can toggle between a rendered view and a plaintext view.
- When a note is not being edited, it should always be shown in rendered form.

## Configuration and Runtime Expectations
- Configuration should be provided through command-line flags, with environment variables available as fallback values.
- Host, port, and watched root directory must all be configurable.
- The default listening port is `4242`.
- Templates and assets should be bundled directly into the application so the final deployment remains self-contained.

## Repository Hygiene
- All repository Go source files should live under the `/src` directory.
- All repository test files should live under the `/test` directory.
- Temporary test artifacts and build output should be kept under `/tmp` so the repository stays clean.

## Visual Theme
Use the Catppuccin Mocha palette throughout the project.

### Design Philosophy
- The interface should follow a cell-shaded visual language.
- Depth should come from crisp, high-contrast black shadows rather than soft blur.
- Layered surfaces should feel like stacked cutout panels with clear separation.
- Menus, headers, cards, buttons, and popouts should reinforce that sharp, graphic depth treatment.

### Accent Colors
- Rosewater: `#f5e0dc`
- Flamingo: `#f2cdcd`
- Pink: `#f5c2e7`
- Mauve: `#cba6f7`
- Red: `#f38ba8`
- Maroon: `#eba0ac`
- Peach: `#fab387`
- Yellow: `#f9e2af`
- Green: `#a6e3a1`
- Teal: `#94e2d5`
- Sky: `#89dceb`
- Sapphire: `#74c7ec`
- Blue: `#89b4fa`
- Lavender: `#b4befe`

### Text and Layer Colors
- Text: `#cdd6f4`
- Subtext 1: `#bac2de`
- Subtext 0: `#a6adc8`
- Overlay 2: `#9399b2`
- Overlay 1: `#7f849c`
- Overlay 0: `#6c7086`
- Surface 2: `#585b70`
- Surface 1: `#45475a`
- Surface 0: `#313244`
- Base: `#1e1e2e`
- Mantle: `#181825`
- Crust: `#11111b`

## Decision Filter
When making future product decisions, prefer choices that:
- preserve plaintext note ownership,
- keep the app responsive on mobile and desktop,
- maintain a clean filesystem-mirroring mental model,
- support a self-contained deployment,
- and reinforce a simple modern interface built around Catppuccin Mocha.
