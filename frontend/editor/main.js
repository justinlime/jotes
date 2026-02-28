import { catppuccinMocha } from "@catppuccin/codemirror";
import { closeBrackets, closeBracketsKeymap } from "@codemirror/autocomplete";
import { defaultKeymap, history, historyKeymap, indentWithTab } from "@codemirror/commands";
import { cpp } from "@codemirror/lang-cpp";
import { css } from "@codemirror/lang-css";
import { go } from "@codemirror/lang-go";
import { html } from "@codemirror/lang-html";
import { javascript } from "@codemirror/lang-javascript";
import { json } from "@codemirror/lang-json";
import { markdown } from "@codemirror/lang-markdown";
import { python } from "@codemirror/lang-python";
import { rust } from "@codemirror/lang-rust";
import { sql } from "@codemirror/lang-sql";
import { xml } from "@codemirror/lang-xml";
import { yaml } from "@codemirror/lang-yaml";
import {
  LanguageDescription,
  LanguageSupport,
  StreamLanguage,
  bracketMatching,
  indentOnInput,
} from "@codemirror/language";
import { clike } from "@codemirror/legacy-modes/mode/clike";
import { clojure } from "@codemirror/legacy-modes/mode/clojure";
import { cmake } from "@codemirror/legacy-modes/mode/cmake";
import { commonLisp } from "@codemirror/legacy-modes/mode/commonlisp";
import { diff } from "@codemirror/legacy-modes/mode/diff";
import { dockerFile } from "@codemirror/legacy-modes/mode/dockerfile";
import { groovy } from "@codemirror/legacy-modes/mode/groovy";
import { lua } from "@codemirror/legacy-modes/mode/lua";
import { powerShell } from "@codemirror/legacy-modes/mode/powershell";
import { properties } from "@codemirror/legacy-modes/mode/properties";
import { protobuf } from "@codemirror/legacy-modes/mode/protobuf";
import { ruby } from "@codemirror/legacy-modes/mode/ruby";
import { shell } from "@codemirror/legacy-modes/mode/shell";
import { swift } from "@codemirror/legacy-modes/mode/swift";
import { tcl } from "@codemirror/legacy-modes/mode/tcl";
import { toml } from "@codemirror/legacy-modes/mode/toml";
import { highlightSelectionMatches, searchKeymap } from "@codemirror/search";
import { Compartment, EditorState } from "@codemirror/state";
import {
  EditorView,
  crosshairCursor,
  drawSelection,
  dropCursor,
  highlightActiveLine,
  highlightActiveLineGutter,
  keymap,
  lineNumbers,
  rectangularSelection,
} from "@codemirror/view";
import { Vim, getCM, vim } from "@replit/codemirror-vim";

const editorRoot = document.getElementById("editor-root");
const editorDocumentBootstrap = editorRoot ? document.getElementById("editor-initial-document") : null;
const editorSurface = editorRoot ? document.getElementById("editor-surface") : null;
const editorRenderOutput = editorRoot ? document.getElementById("editor-render-output") : null;
const editorSaveButton = editorRoot ? document.getElementById("editor-save-btn") : null;
const defaultEditorSaveButtonLabel = editorSaveButton ? (editorSaveButton.textContent || "").trim() || "Save" : "Save";
const editorViewToggleButton = editorRoot ? document.getElementById("editor-view-toggle-btn") : null;
const editorPlainPanel = editorRoot ? document.getElementById("editor-panel-plain") : null;
const editorRenderedPanel = editorRoot ? document.getElementById("editor-panel-rendered") : null;
const editorSettingsMenu = editorRoot ? document.getElementById("editor-settings") : null;
const editorKeymapRadios = editorRoot ? document.querySelectorAll('input[name="editor-setting-keymap"]') : [];
const editorLineWrappingInput = editorRoot ? document.getElementById("editor-setting-line-wrapping") : null;
const editorLineNumbersInput = editorRoot ? document.getElementById("editor-setting-line-numbers") : null;
const standalonePreviewRoots = Array.from(document.querySelectorAll('[data-codemirror-preview-root][data-content-script-id]'));

const editorCookieNames = Object.freeze({
  keymap: "jotes_editor_keymap",
  lineWrapping: "jotes_editor_line_wrapping",
  lineNumbers: "jotes_editor_line_numbers",
});

const defaultEditorSettings = Object.freeze({
  keymap: "default",
  lineWrapping: true,
  lineNumbers: false,
});

const editorState = {
  bootstrap: null,
  view: null,
  previewView: null,
  settings: null,
  lastSavedContent: "",
  currentRevision: "",
  renderTimer: null,
  renderRequestId: 0,
  saveInFlight: false,
};

const languageCompartment = new Compartment();
const fontCompartment = new Compartment(); // For switching between Inter (Markdown/Org) and FiraCode (other text files)
const vimCompartment = new Compartment();
const lineWrappingCompartment = new Compartment();
const lineNumbersCompartment = new Compartment();



const editorLanguageSupportRegistry = Object.freeze({
  markdown: markdown({
    codeLanguages: resolveCodeBlockLanguageSupport,
  }),
  html: html(),
  xml: xml(),
  json: json(),
  javascript: javascript(),
  jsx: javascript({ jsx: true }),
  typescript: javascript({ typescript: true }),
  tsx: javascript({ jsx: true, typescript: true }),
  css: css(),
  yaml: yaml(),
  cpp: cpp(),
  go: go(),
  python: python(),
  rust: rust(),
  sql: sql(),
  shell: createLegacyLanguageSupport(shell),
  toml: createLegacyLanguageSupport(toml),
  properties: createLegacyLanguageSupport(properties),
  dockerfile: createLegacyLanguageSupport(dockerFile),
  diff: createLegacyLanguageSupport(diff),
  commonLisp: createLegacyLanguageSupport(commonLisp),
  clike: createLegacyLanguageSupport(clike),
  clojure: createLegacyLanguageSupport(clojure),
  cmake: createLegacyLanguageSupport(cmake),
  groovy: createLegacyLanguageSupport(groovy),
  lua: createLegacyLanguageSupport(lua),
  powershell: createLegacyLanguageSupport(powerShell),
  protobuf: createLegacyLanguageSupport(protobuf),
  ruby: createLegacyLanguageSupport(ruby),
  swift: createLegacyLanguageSupport(swift),
  tcl: createLegacyLanguageSupport(tcl),
});

const editorLanguageDescriptions = Object.freeze([
  LanguageDescription.of({
    name: "Markdown",
    alias: ["md", "markdown"],
    extensions: ["md", "markdown"],
    support: editorLanguageSupportRegistry.markdown,
  }),
  LanguageDescription.of({
    name: "reStructuredText",
    alias: ["rst"],
    extensions: ["rst"],
    support: editorLanguageSupportRegistry.markdown,
  }),
  LanguageDescription.of({
    name: "HTML",
    alias: ["htm"],
    extensions: ["html", "htm"],
    support: editorLanguageSupportRegistry.html,
  }),
  LanguageDescription.of({
    name: "XML",
    alias: ["svg"],
    extensions: ["xml", "xsl", "xslt", "svg"],
    support: editorLanguageSupportRegistry.xml,
  }),
  LanguageDescription.of({
    name: "JSON",
    alias: ["json"],
    extensions: ["json"],
    support: editorLanguageSupportRegistry.json,
  }),
  LanguageDescription.of({
    name: "JSONC",
    alias: ["jsonc", "json5"],
    extensions: ["jsonc", "json5"],
    support: editorLanguageSupportRegistry.javascript,
  }),
  LanguageDescription.of({
    name: "JavaScript",
    alias: ["js", "node"],
    extensions: ["js", "mjs", "cjs"],
    support: editorLanguageSupportRegistry.javascript,
  }),
  LanguageDescription.of({
    name: "JSX",
    alias: ["jsx"],
    extensions: ["jsx"],
    support: editorLanguageSupportRegistry.jsx,
  }),
  LanguageDescription.of({
    name: "TypeScript",
    alias: ["ts"],
    extensions: ["ts"],
    support: editorLanguageSupportRegistry.typescript,
  }),
  LanguageDescription.of({
    name: "TSX",
    alias: ["tsx"],
    extensions: ["tsx"],
    support: editorLanguageSupportRegistry.tsx,
  }),
  LanguageDescription.of({
    name: "CSS",
    alias: ["css"],
    extensions: ["css", "scss", "less"],
    support: editorLanguageSupportRegistry.css,
  }),
  LanguageDescription.of({
    name: "YAML",
    alias: ["yaml", "yml"],
    extensions: ["yaml", "yml"],
    support: editorLanguageSupportRegistry.yaml,
  }),
  LanguageDescription.of({
    name: "TOML",
    alias: ["toml"],
    extensions: ["toml"],
    support: editorLanguageSupportRegistry.toml,
  }),
  LanguageDescription.of({
    name: "Go",
    alias: ["go"],
    extensions: ["go"],
    support: editorLanguageSupportRegistry.go,
  }),
  LanguageDescription.of({
    name: "C++",
    alias: ["c", "cpp", "cxx", "cc", "hpp", "hxx", "h"],
    extensions: ["c", "cpp", "cxx", "cc", "hpp", "hxx", "h"],
    support: editorLanguageSupportRegistry.cpp,
  }),
  LanguageDescription.of({
    name: "Java",
    alias: ["java", "kotlin", "kt", "scala", "csharp", "cs"],
    extensions: ["java", "kt", "kts", "scala", "cs"],
    support: editorLanguageSupportRegistry.clike,
  }),
  LanguageDescription.of({
    name: "Python",
    alias: ["py"],
    extensions: ["py"],
    support: editorLanguageSupportRegistry.python,
  }),
  LanguageDescription.of({
    name: "Ruby",
    alias: ["rb", "jruby", "rake"],
    extensions: ["rb"],
    filename: /^(?:gemfile|rakefile|guardfile|capfile|appfile|fastfile)$/i,
    support: editorLanguageSupportRegistry.ruby,
  }),
  LanguageDescription.of({
    name: "Lua",
    alias: ["lua"],
    extensions: ["lua"],
    filename: /^init\.lua$/i,
    support: editorLanguageSupportRegistry.lua,
  }),
  LanguageDescription.of({
    name: "Rust",
    alias: ["rs"],
    extensions: ["rs"],
    support: editorLanguageSupportRegistry.rust,
  }),
  LanguageDescription.of({
    name: "SQL",
    alias: ["sql"],
    extensions: ["sql"],
    support: editorLanguageSupportRegistry.sql,
  }),
  LanguageDescription.of({
    name: "Clojure",
    alias: ["clojure", "clj", "cljs", "cljc"],
    extensions: ["clj", "cljs", "cljc"],
    support: editorLanguageSupportRegistry.clojure,
  }),
  LanguageDescription.of({
    name: "Groovy",
    alias: ["groovy", "gradle", "jenkinsfile"],
    extensions: ["groovy", "gradle"],
    filename: /^jenkinsfile$/i,
    support: editorLanguageSupportRegistry.groovy,
  }),
  LanguageDescription.of({
    name: "PowerShell",
    alias: ["powershell", "ps"],
    extensions: ["ps1", "psd1", "psm1"],
    support: editorLanguageSupportRegistry.powershell,
  }),
  LanguageDescription.of({
    name: "Protobuf",
    alias: ["protobuf", "proto"],
    extensions: ["proto"],
    support: editorLanguageSupportRegistry.protobuf,
  }),
  LanguageDescription.of({
    name: "Swift",
    alias: ["swift"],
    extensions: ["swift"],
    support: editorLanguageSupportRegistry.swift,
  }),
  LanguageDescription.of({
    name: "Tcl",
    alias: ["tcl"],
    extensions: ["tcl"],
    support: editorLanguageSupportRegistry.tcl,
  }),
  LanguageDescription.of({
    name: "CMake",
    alias: ["cmake"],
    extensions: ["cmake"],
    filename: /^cmakelists\.txt$/i,
    support: editorLanguageSupportRegistry.cmake,
  }),
  LanguageDescription.of({
    name: "Shell",
    alias: ["sh", "bash", "zsh", "ksh"],
    extensions: ["sh", "bash", "zsh", "ksh"],
    filename: /^(?:\.envrc|\.bashrc|\.zshrc|\.profile|\.bash_profile|\.bash_aliases)$/i,
    support: editorLanguageSupportRegistry.shell,
  }),
  LanguageDescription.of({
    name: "Dockerfile",
    alias: ["dockerfile", "containerfile"],
    filename: /^(?:dockerfile|containerfile)$/i,
    support: editorLanguageSupportRegistry.dockerfile,
  }),
  LanguageDescription.of({
    name: "Properties",
    alias: ["ini", "cfg", "conf", "properties", "editorconfig"],
    extensions: ["ini", "cfg", "conf", "properties"],
    filename: /^\.editorconfig$/i,
    support: editorLanguageSupportRegistry.properties,
  }),
  LanguageDescription.of({
    name: "Diff",
    alias: ["patch"],
    extensions: ["diff", "patch"],
    support: editorLanguageSupportRegistry.diff,
  }),
  LanguageDescription.of({
    name: "Lisp",
    alias: ["lisp", "common lisp", "emacs lisp", "elisp"],
    extensions: ["lisp", "cl", "el", "elisp"],
    support: editorLanguageSupportRegistry.commonLisp,
  }),
]);

const editorLanguageMimeTypeMap = new Map([
  ["text/markdown", editorLanguageSupportRegistry.markdown],
  ["text/x-rst", editorLanguageSupportRegistry.markdown],
  ["text/html", editorLanguageSupportRegistry.html],
  ["text/xml", editorLanguageSupportRegistry.xml],
  ["application/xml", editorLanguageSupportRegistry.xml],
  ["image/svg+xml", editorLanguageSupportRegistry.xml],
  ["application/json", editorLanguageSupportRegistry.json],
  ["text/css", editorLanguageSupportRegistry.css],
  ["text/yaml", editorLanguageSupportRegistry.yaml],
  ["text/x-toml", editorLanguageSupportRegistry.toml],
  ["text/javascript", editorLanguageSupportRegistry.javascript],
  ["application/javascript", editorLanguageSupportRegistry.javascript],
  ["text/typescript", editorLanguageSupportRegistry.typescript],
  ["text/x-go", editorLanguageSupportRegistry.go],
  ["text/x-csrc", editorLanguageSupportRegistry.cpp],
  ["text/x-c++src", editorLanguageSupportRegistry.cpp],
  ["text/x-java", editorLanguageSupportRegistry.clike],
  ["text/x-kotlin", editorLanguageSupportRegistry.clike],
  ["text/x-scala", editorLanguageSupportRegistry.clike],
  ["text/x-csharp", editorLanguageSupportRegistry.clike],
  ["text/x-python", editorLanguageSupportRegistry.python],
  ["text/x-rust", editorLanguageSupportRegistry.rust],
  ["text/x-ruby", editorLanguageSupportRegistry.ruby],
  ["text/x-lua", editorLanguageSupportRegistry.lua],
  ["text/x-sql", editorLanguageSupportRegistry.sql],
  ["text/x-clojure", editorLanguageSupportRegistry.clojure],
  ["text/x-groovy", editorLanguageSupportRegistry.groovy],
  ["text/x-protobuf", editorLanguageSupportRegistry.protobuf],
  ["application/x-powershell", editorLanguageSupportRegistry.powershell],
  ["text/x-sh", editorLanguageSupportRegistry.shell],
  ["application/x-sh", editorLanguageSupportRegistry.shell],
  ["text/x-cmake", editorLanguageSupportRegistry.cmake],
  ["text/x-ini", editorLanguageSupportRegistry.properties],
  ["text/x-java-properties", editorLanguageSupportRegistry.properties],
  ["text/x-diff", editorLanguageSupportRegistry.diff],
  ["text/x-elisp", editorLanguageSupportRegistry.commonLisp],
]);

let vimCommandsRegistered = false;

/**
 * Initialize the editor page by reading the server-rendered bootstrap data,
 * restoring cookie-backed settings, creating the CodeMirror 6 instance, and
 * wiring all page-level events.
 *
 * Parameters:
 *   - none: the function reads the editor DOM nodes and data attributes declared at module scope.
 *
 * Returns:
 *   - none: the CodeMirror editor is mounted in place when the page is present, otherwise the function exits quietly.
 */
function initializeEditorPage() {
  if (!editorRoot || !editorDocumentBootstrap || !editorSurface || !editorRenderOutput || !editorSaveButton || !editorViewToggleButton || !editorPlainPanel || !editorRenderedPanel || !editorLineWrappingInput || !editorLineNumbersInput) {
    return;
  }

  editorState.bootstrap = readEditorBootstrap();
  editorState.settings = loadEditorSettings();
  editorState.lastSavedContent = editorState.bootstrap.initialContent;
  editorState.currentRevision = editorState.bootstrap.revision;

  applySettingsToControls(editorState.settings);
  registerVimCommands();
  buildEditorView();
  bindPageEvents();
  bindSettingsControls();
  setMode("plain");
}

/**
 * Read the server-rendered editor metadata and initial document text from the
 * page so the client can bootstrap the CodeMirror instance without relying on
 * a hidden textarea fallback.
 *
 * Parameters:
 *   - none: the function reads editorRoot dataset values and the JSON bootstrap script content.
 *
 * Returns:
 *   - object: the parsed editor bootstrap payload including note paths, save/render/upload endpoints, MIME type, revision, and initial document text.
 */
function readEditorBootstrap() {
  return {
    filePath: editorRoot.dataset.filePath || "",
    mimeType: editorRoot.dataset.mimeType || "",
    saveUrl: editorRoot.dataset.saveUrl || "",
    renderUrl: editorRoot.dataset.renderUrl || "",
    imageUploadUrl: editorRoot.dataset.imageUploadUrl || "",
    renderMode: editorRoot.dataset.renderMode || "document",
    revision: editorRoot.dataset.revision || "",
    initialContent: parseBootstrapDocumentContent(),
  };
}

/**
 * Parse the initial editor document body from the JSON bootstrap script that
 * the template emits for the current note.
 *
 * Parameters:
 *   - none: the function reads the text content of the editorDocumentBootstrap script element.
 *
 * Returns:
 *   - string: the decoded initial note content, or an empty string when parsing fails.
 */
function parseBootstrapDocumentContent() {
  return parseJSONScriptContent(editorDocumentBootstrap);
}

/**
 * Decode one application/json bootstrap script element into a string payload.
 *
 * Parameters:
 *   - scriptElement: the script node whose text content should contain a JSON-encoded string.
 *
 * Returns:
 *   - string: the decoded string value, or an empty string when the node is missing or malformed.
 */
function parseJSONScriptContent(scriptElement) {
  if (!scriptElement) {
    return "";
  }

  try {
    return JSON.parse(scriptElement.textContent || '""');
  } catch (error) {
    return "";
  }
}

/**
 * Initialize every standalone read-only CodeMirror preview that appears on a
 * normal file preview page outside the full editor shell.
 *
 * Parameters:
 *   - none: the function reads the preview root elements discovered at module scope.
 *
 * Returns:
 *   - none: each eligible preview root is replaced with a read-only CodeMirror instance.
 */
function initializeStandaloneCodeMirrorPreviews() {
  standalonePreviewRoots.forEach(initializeStandaloneCodeMirrorPreview);
}

/**
 * Mount one standalone read-only CodeMirror preview into the provided preview
 * container when valid bootstrap data is present.
 *
 * Parameters:
 *   - previewRoot: the DOM element that should host the read-only preview instance.
 *
 * Returns:
 *   - none: the preview root is populated in place when bootstrap data is available.
 */
function initializeStandaloneCodeMirrorPreview(previewRoot) {
  const bootstrap = readStandalonePreviewBootstrap(previewRoot);

  if (!bootstrap) {
    return;
  }

  createReadOnlyPreviewView(previewRoot, bootstrap.initialContent, bootstrap.filePath, bootstrap.mimeType, bootstrap.options);
}

/**
 * Read the bootstrap metadata embedded next to one standalone preview root so
 * the page can build a read-only CodeMirror preview without server-side HTML
 * syntax highlighting.
 *
 * Parameters:
 *   - previewRoot: the DOM element that exposes the preview data attributes.
 *
 * Returns:
 *   - object|null: the parsed preview bootstrap payload, or null when the referenced content script is missing.
 */
function readStandalonePreviewBootstrap(previewRoot) {
  const contentScriptId = previewRoot.dataset.contentScriptId || "";
  const contentScript = contentScriptId ? document.getElementById(contentScriptId) : null;
  const settings = loadEditorSettings();

  if (!contentScript) {
    return null;
  }

  return {
    filePath: previewRoot.dataset.filePath || "",
    mimeType: previewRoot.dataset.mimeType || "",
    initialContent: parseJSONScriptContent(contentScript),
    options: createReadOnlyPreviewOptions(settings, previewRoot.getAttribute("aria-label") || "Read-only note preview"),
  };
}

/**
 * Build the display-options object shared by read-only CodeMirror previews so
 * they follow the same line-number and wrapping preferences as the editor.
 *
 * Parameters:
 *   - settings: the normalized editor settings that supply line-number and wrapping preferences.
 *   - ariaLabel: the accessible label text that should be applied to the preview surface.
 *
 * Returns:
 *   - object: the preview options object expected by createReadOnlyPreviewView.
 */
function createReadOnlyPreviewOptions(settings, ariaLabel) {
  return {
    lineNumbers: settings.lineNumbers,
    lineWrapping: settings.lineWrapping,
    ariaLabel: ariaLabel || "Read-only note preview",
  };
}

/**
 * Create one read-only CodeMirror view that mirrors a plaintext note using the
 * same language-detection registry as the editable editor.
 *
 * Parameters:
 *   - parent: the DOM element that should host the preview instance.
 *   - content: the plaintext note body to show in the preview.
 *   - filePath: the note path used for file-name-based language detection.
 *   - mimeType: the detected MIME type used for fallback language detection and font selection.
 *   - options: preview display options including line numbers, line wrapping, and aria label text.
 *
 * Returns:
 *   - EditorView: the mounted read-only CodeMirror preview view.
 */
function createReadOnlyPreviewView(parent, content, filePath, mimeType, options) {
  return new EditorView({
    state: EditorState.create({
      doc: String(content || ""),
      extensions: createReadOnlyPreviewExtensions(filePath, mimeType, options),
    }),
    parent,
  });
}

/**
 * Build the CodeMirror extension set used by read-only previews for non-Markdown
 * and non-Org text files.
 *
 * Parameters:
 *   - filePath: the note path used for file-name-based language detection.
 *   - mimeType: the detected MIME type used for fallback language detection and font selection.
 *   - options: preview display options including line numbers, line wrapping, and aria label text.
 *
 * Returns:
 *   - Array: the complete extension list for one read-only preview instance.
 */
function createReadOnlyPreviewExtensions(filePath, mimeType, options) {
  const extensions = [
    EditorState.readOnly.of(true),
    EditorState.tabSize.of(2),
    EditorState.allowMultipleSelections.of(true),
    EditorView.editable.of(false),
    EditorView.contentAttributes.of({
      spellcheck: "false",
      autocapitalize: "off",
      autocomplete: "off",
      autocorrect: "off",
      "aria-label": options.ariaLabel || "Read-only note preview",
    }),
    createFontExtension(mimeType),
    createLanguageExtension(filePath, mimeType),
    drawSelection(),
    rectangularSelection(),
    catppuccinMocha,
  ];

  if (options.lineNumbers) {
    extensions.push(lineNumbers(), highlightActiveLineGutter());
  }

  if (options.lineWrapping) {
    extensions.push(EditorView.lineWrapping);
  }

  return extensions;
}

/**
 * Replace the entire contents of one read-only CodeMirror preview while
 * preserving the existing mounted view instance.
 *
 * Parameters:
 *   - previewView: the preview EditorView whose document should be replaced.
 *   - content: the next plaintext note body to display.
 *
 * Returns:
 *   - none: the preview document is updated in place when the content changed.
 */
function replaceReadOnlyPreviewContent(previewView, content) {
  const nextContent = String(content || "");
  const currentContent = previewView.state.doc.toString();

  if (currentContent === nextContent) {
    return;
  }

  previewView.dispatch({
    changes: {
      from: 0,
      to: currentContent.length,
      insert: nextContent,
    },
  });
}

/**
 * Build the CodeMirror 6 editor view using the active note content, language,
 * user settings, and pasted-image DOM handlers for Markdown and Org notes.
 *
 * Parameters:
 *   - none: the function reads editorState.bootstrap and editorState.settings.
 *
 * Returns:
 *   - none: editorState.view is created and mounted into the editor surface.
 */
function buildEditorView() {
  editorState.view = new EditorView({
    state: EditorState.create({
      doc: editorState.bootstrap.initialContent,
      extensions: [
        EditorState.tabSize.of(2),
        EditorState.allowMultipleSelections.of(true),
        EditorView.contentAttributes.of({
          spellcheck: "false",
          autocapitalize: "off",
          autocomplete: "off",
          autocorrect: "off",
          "aria-label": "Plaintext note editor",
        }),
        fontCompartment.of(createFontExtension(editorState.bootstrap.mimeType)),
        vimCompartment.of(createVimExtension(editorState.settings.keymap)),
        lineWrappingCompartment.of(createLineWrappingExtension(editorState.settings.lineWrapping)),
        lineNumbersCompartment.of(createLineNumberExtensions(editorState.settings.lineNumbers)),
        languageCompartment.of(createLanguageExtension(editorState.bootstrap.filePath, editorState.bootstrap.mimeType)),
        history(),
        drawSelection(),
        dropCursor(),
        indentOnInput(),
        bracketMatching(),
        closeBrackets(),
        rectangularSelection(),
        crosshairCursor(),
        highlightActiveLine(),
        highlightSelectionMatches(),
        catppuccinMocha,
        EditorView.domEventHandlers({
          paste: handleEditorPaste,
        }),
        EditorView.updateListener.of(handleEditorViewUpdate),
        keymap.of(createEditorKeybindings()),
      ],
    }),
    parent: editorSurface,
  });
}

/**
 * Create the shared non-Vim keybindings that should remain available in the
 * editor, including save, search, history, and standard movement commands.
 *
 * Parameters:
 *   - none: the function reads no external state.
 *
 * Returns:
 *   - Array<object>: the ordered CodeMirror keybinding list applied to the editor.
 */
function createEditorKeybindings() {
  return closeBracketsKeymap.concat(defaultKeymap, searchKeymap, historyKeymap, [indentWithTab]);
}

/**
 * Convert a legacy stream parser into a CodeMirror 6 language support object
 * so older tokenizers can still power highlighting for formats without a
 * dedicated Lezer package in this integration.
 *
 * Parameters:
 *   - streamParser: the legacy mode parser imported from @codemirror/legacy-modes.
 *
 * Returns:
 *   - LanguageSupport: a CM6-compatible wrapper around the supplied stream parser.
 */
function createLegacyLanguageSupport(streamParser) {
  return new LanguageSupport(StreamLanguage.define(streamParser));
}

/**
 * Resolve the best highlighting support for a fenced Markdown code block info
 * string using the same language registry as the main editor.
 *
 * Parameters:
 *   - info: the fenced code block language label written in the Markdown source.
 *
 * Returns:
 *   - LanguageSupport|null: the matching language support when one is known, otherwise null.
 */
function resolveCodeBlockLanguageSupport(info) {
  const description = LanguageDescription.matchLanguageName(editorLanguageDescriptions, String(info || ""), true);

  if (!description || !description.support) {
    return null;
  }

  return description.support;
}

/**
 * Create the font extension for the editor based on MIME type.
 * Markdown/Org files use Inter (sans-serif), all other text files use FiraCode (monospace).
 *
 * Parameters:
 *   - mimeType: the server-detected MIME type for the note.
 *
 * Returns:
 *   - EditorView.theme: a theme extension that sets the appropriate font family for .cm-scroller.
 */
function createFontExtension(mimeType) {
  const normalizedMimeType = normalizeMimeType(mimeType);
  
  // Use Inter (sans-serif) for Markdown and Org-mode, FiraCode (monospace) for everything else
  if (normalizedMimeType === "text/markdown" || normalizedMimeType === "text/x-org") {
    return EditorView.theme({
      ".cm-scroller": {
        fontFamily: "var(--font-sans)", // Inter
      },
    });
  }
  
  // Default to FiraCode for all other text files (JSON, YAML, Go, Python, etc.)
  return EditorView.theme({
    ".cm-scroller": {
      fontFamily: "var(--font-mono)", // FiraCode
    },
  });
}

/**
 * Create the language extension that should be installed for the current note
 * based on its file name and detected MIME type.
 *
 * Parameters:
 *   - filePath: the note path currently being edited.
 *   - mimeType: the server-detected MIME type for the note.
 *
 * Returns:
 *   - Array|LanguageSupport: the resolved language extension, or an empty array when the editor should remain plain text.
 */
function createLanguageExtension(filePath, mimeType) {
  const support = resolveLanguageSupportForEditorFile(filePath, mimeType);

  if (!support) {
    return [];
  }

  return support;
}

/**
 * Resolve the most appropriate language support for one editor file by first
 * checking the file name and then falling back to MIME type heuristics.
 *
 * Parameters:
 *   - filePath: the note path currently being edited.
 *   - mimeType: the server-detected MIME type for the note.
 *
 * Returns:
 *   - LanguageSupport|null: the matching language support, or null when plain text should be used.
 */
function resolveLanguageSupportForEditorFile(filePath, mimeType) {
  const fileName = getEditorFileName(filePath);
  const description = fileName ? LanguageDescription.matchFilename(editorLanguageDescriptions, fileName) : null;
  const normalizedMimeType = normalizeMimeType(mimeType);

  if (description && description.support) {
    return description.support;
  }

  if (!normalizedMimeType) {
    return null;
  }

  return editorLanguageMimeTypeMap.get(normalizedMimeType) || null;
}

/**
 * Extract the file name portion from an editor path so language detection can
 * apply filename and extension-based heuristics.
 *
 * Parameters:
 *   - filePath: the current editor path.
 *
 * Returns:
 *   - string: the trailing file name segment, or an empty string when the path is missing.
 */
function getEditorFileName(filePath) {
  const segments = String(filePath || "").split("/");

  return segments[segments.length - 1] || "";
}

/**
 * Normalize one MIME type by discarding any charset parameters and converting
 * the remaining type token to lowercase.
 *
 * Parameters:
 *   - mimeType: the raw MIME type string from the DOM dataset.
 *
 * Returns:
 *   - string: the normalized MIME type token, or an empty string when the input is blank.
 */
function normalizeMimeType(mimeType) {
  return String(mimeType || "").split(";", 1)[0].trim().toLowerCase();
}

/**
 * Report whether the current note type supports pasted-image uploads into the
 * managed sibling companion directory flow.
 *
 * Parameters:
 *   - mimeType: the note MIME type that should be checked for pasted-image support.
 *
 * Returns:
 *   - boolean: true for Markdown and Org notes, otherwise false.
 */
function editorSupportsPastedImageAssets(mimeType) {
  const normalizedMimeType = normalizeMimeType(mimeType);

  return normalizedMimeType === "text/markdown" || normalizedMimeType === "text/x-org";
}

/**
 * Collect every clipboard File object that represents an image paste payload so
 * the editor can upload each image instead of inserting raw clipboard text.
 *
 * Parameters:
 *   - event: the ClipboardEvent raised by the browser for the current paste action.
 *
 * Returns:
 *   - Array<File>: each image file discovered in the clipboard payload, in clipboard order.
 */
function extractClipboardImageFiles(event) {
  const clipboardData = event && event.clipboardData ? event.clipboardData : null;
  const imageFiles = [];

  if (!clipboardData) {
    return imageFiles;
  }

  if (clipboardData.items && clipboardData.items.length > 0) {
    Array.from(clipboardData.items).forEach(function (item) {
      const clipboardFile = item && item.kind === "file" && String(item.type || "").toLowerCase().startsWith("image/") ? item.getAsFile() : null;

      if (clipboardFile) {
        imageFiles.push(clipboardFile);
      }
    });
  }

  if (imageFiles.length > 0) {
    return imageFiles;
  }

  if (clipboardData.files && clipboardData.files.length > 0) {
    Array.from(clipboardData.files).forEach(function (file) {
      if (file && String(file.type || "").toLowerCase().startsWith("image/")) {
        imageFiles.push(file);
      }
    });
  }

  return imageFiles;
}

/**
 * Upload one pasted clipboard image to the server-side managed companion
 * directory for the current note.
 *
 * Parameters:
 *   - file: the clipboard File object that should be uploaded.
 *
 * Returns:
 *   - Promise<object>: resolves with the JSON API payload describing the stored asset path and relative note markup path.
 */
async function uploadPastedEditorImage(file) {
  const uploadUrl = String(editorState.bootstrap && editorState.bootstrap.imageUploadUrl ? editorState.bootstrap.imageUploadUrl : "");
  const uploadFileName = file && file.name ? file.name : "pasted-image";
  const requestBody = new FormData();
  let response;

  if (!uploadUrl) {
    throw new Error("Pasted image uploads are unavailable for this note.");
  }

  requestBody.append("path", editorState.bootstrap.filePath || "");
  requestBody.append("file", file, uploadFileName);

  response = await fetch(uploadUrl, {
    method: "POST",
    headers: {
      "X-Jotes-Editor": "1",
    },
    body: requestBody,
  });

  return readEditorJSON(response, "Pasted image upload failed.");
}

/**
 * Build the note-source markup that should be inserted after one pasted image
 * upload completes successfully.
 *
 * Parameters:
 *   - mimeType: the MIME type of the note currently being edited.
 *   - relativePath: the note-relative asset path returned by the upload API.
 *
 * Returns:
 *   - string: the Markdown or Org image markup that should be inserted into the editor document.
 */
function buildEditorPastedImageMarkup(mimeType, relativePath) {
  const normalizedMimeType = normalizeMimeType(mimeType);

  if (normalizedMimeType === "text/x-org") {
    return "[[file:" + String(relativePath || "") + "]]";
  }

  return "![Pasted image](<" + String(relativePath || "") + ">)";
}

/**
 * Replace the current primary selection in the editor with the supplied text
 * and leave the caret at the end of the inserted content.
 *
 * Parameters:
 *   - view: the CodeMirror editor view whose document should receive the inserted text.
 *   - text: the exact text that should replace the current primary selection.
 *
 * Returns:
 *   - none: the editor document and selection are updated in place.
 */
function insertTextIntoEditorSelection(view, text) {
  const selection = view.state.selection.main;
  const insertedText = String(text || "");

  view.dispatch({
    changes: {
      from: selection.from,
      to: selection.to,
      insert: insertedText,
    },
    selection: {
      anchor: selection.from + insertedText.length,
    },
    scrollIntoView: true,
  });
}

/**
 * Upload one or more pasted clipboard images, build the note markup for every
 * successful upload, and insert the resulting references into the editor.
 *
 * Parameters:
 *   - view: the CodeMirror editor view that received the original paste event.
 *   - imageFiles: the clipboard image files that should be uploaded in order.
 *
 * Returns:
 *   - Promise<void>: resolves after any successful markup insertions and error reporting complete.
 */
async function pasteClipboardImagesIntoEditor(view, imageFiles) {
  const markupFragments = [];
  let uploadError = null;

  for (const imageFile of imageFiles) {
    try {
      const payload = await uploadPastedEditorImage(imageFile);
      const relativePath = typeof payload.relativePath === "string" ? payload.relativePath.trim() : "";

      if (!relativePath) {
        throw new Error("Jotes did not return a pasted image path.");
      }

      markupFragments.push(buildEditorPastedImageMarkup(editorState.bootstrap.mimeType, relativePath));
    } catch (error) {
      uploadError = error;
      break;
    }
  }

  if (markupFragments.length > 0) {
    insertTextIntoEditorSelection(view, markupFragments.join("\n"));
    view.focus();
  }

  if (uploadError) {
    showEditorToastNotification("error", uploadError.message || "Pasted image upload failed.");
  }
}

/**
 * Intercept clipboard paste events when the current note supports pasted-image
 * uploads and the clipboard contains image data.
 *
 * Parameters:
 *   - event: the ClipboardEvent raised by the editor DOM surface.
 *   - view: the CodeMirror editor view that received the paste.
 *
 * Returns:
 *   - boolean: true when the paste was handled as an uploaded image flow, otherwise false.
 */
function handleEditorPaste(event, view) {
  const imageFiles = extractClipboardImageFiles(event);

  if (!editorSupportsPastedImageAssets(editorState.bootstrap.mimeType) || imageFiles.length === 0) {
    return false;
  }

  event.preventDefault();
  void pasteClipboardImagesIntoEditor(view, imageFiles);
  return true;
}

/**
 * Route Escape presses through the Vim compatibility layer so insert mode
 * reliably returns to normal mode without leaving the active Vim keymap.
 *
 * Parameters:
 *   - event: the DOM keyboard event raised by the editor view.
 *   - view: the CodeMirror editor view that received the key event.
 *
 * Returns:
 *   - boolean: true when the Escape key was handled for Vim mode, otherwise false.
 */
function handleVimEscapeKeydown(event, view) {
  const codeMirrorCompatibilityLayer = getCM(view);

  if (event.key !== "Escape" || !codeMirrorCompatibilityLayer) {
    return false;
  }

  event.preventDefault();
  event.stopPropagation();
  Vim.handleKey(codeMirrorCompatibilityLayer, "<Esc>", "user");
  return true;
}

/**
 * Create the optional Vim-related extensions that should sit before the normal
 * keymaps so Vim bindings can take precedence when enabled.
 *
 * Parameters:
 *   - keymapName: the user-selected editor keymap identifier.
 *
 * Returns:
 *   - Array: the Vim extensions when enabled, otherwise an empty array.
 */
function createVimExtension(keymapName) {
  if (keymapName !== "vim") {
    return [];
  }

  return [
    vim(),
    EditorView.domEventHandlers({
      keydown: handleVimEscapeKeydown,
    }),
  ];
}

/**
 * Create the optional line-wrapping extension for the editor based on the
 * current settings menu state.
 *
 * Parameters:
 *   - lineWrappingEnabled: whether soft wrapping should be enabled in the editor.
 *
 * Returns:
 *   - Array: the line-wrapping extension when enabled, otherwise an empty array.
 */
function createLineWrappingExtension(lineWrappingEnabled) {
  if (!lineWrappingEnabled) {
    return [];
  }

  return [EditorView.lineWrapping];
}

/**
 * Create the optional line-number gutter extensions for the current settings
 * state.
 *
 * Parameters:
 *   - lineNumbersEnabled: whether line numbers should be visible.
 *
 * Returns:
 *   - Array: the gutter extensions when enabled, otherwise an empty array.
 */
function createLineNumberExtensions(lineNumbersEnabled) {
  if (!lineNumbersEnabled) {
    return [];
  }

  return [lineNumbers(), highlightActiveLineGutter()];
}

/**
 * Register page-level events for view toggling, saving, keyboard shortcuts,
 * and unload protection.
 *
 * Parameters:
 *   - none: the function reads the shared DOM references declared at module scope.
 *
 * Returns:
 *   - none: event listeners are attached to the editor controls and window.
 */
function bindPageEvents() {
  editorViewToggleButton.addEventListener("click", handleEditorViewToggleButtonClick);
  editorSaveButton.addEventListener("click", handleSaveButtonClick);
  document.addEventListener("keydown", handleSaveShortcut);
  window.addEventListener("beforeunload", handleBeforeUnload);
}

/**
 * Register settings-control listeners so cookie-backed editor preferences can
 * update the live CodeMirror instance immediately.
 *
 * Parameters:
 *   - none: the function reads the settings inputs declared at module scope.
 *
 * Returns:
 *   - none: each settings control is wired to the shared settings change handler.
 */
function bindSettingsControls() {
  Array.from(editorKeymapRadios).forEach(bindKeymapRadioInput);
  editorLineWrappingInput.addEventListener("change", handleLineWrappingInputChange);
  editorLineNumbersInput.addEventListener("change", handleLineNumbersInputChange);
}

/**
 * Attach the shared keymap change handler to one settings radio input.
 *
 * Parameters:
 *   - radioInput: the radio element that selects an editor keymap option.
 *
 * Returns:
 *   - none: the change handler is attached to radioInput.
 */
function bindKeymapRadioInput(radioInput) {
  radioInput.addEventListener("change", handleKeymapInputChange);
}

/**
 * Restore the persisted editor settings from browser cookies and normalize the
 * result against the supported settings values.
 *
 * Parameters:
 *   - none: the function reads document.cookie.
 *
 * Returns:
 *   - object: the normalized editor settings object used for initial UI and editor configuration.
 */
function loadEditorSettings() {
  return normalizeEditorSettings({
    keymap: readCookieValue(editorCookieNames.keymap),
    lineWrapping: parseBooleanCookieValue(readCookieValue(editorCookieNames.lineWrapping), defaultEditorSettings.lineWrapping),
    lineNumbers: parseBooleanCookieValue(readCookieValue(editorCookieNames.lineNumbers), defaultEditorSettings.lineNumbers),
  });
}

/**
 * Normalize a possibly incomplete settings object into the supported Jotes
 * editor settings shape.
 *
 * Parameters:
 *   - settings: the raw settings values loaded from cookies or form controls.
 *
 * Returns:
 *   - object: the normalized settings object containing only supported values.
 */
function normalizeEditorSettings(settings) {
  return {
    keymap: normalizeEditorKeymap(settings.keymap),
    lineWrapping: Boolean(settings.lineWrapping),
    lineNumbers: Boolean(settings.lineNumbers),
  };
}

/**
 * Normalize the keymap setting to one of the supported identifiers.
 *
 * Parameters:
 *   - keymapName: the raw keymap identifier loaded from cookies or controls.
 *
 * Returns:
 *   - string: either "default" or "vim".
 */
function normalizeEditorKeymap(keymapName) {
  if (String(keymapName || "").toLowerCase() === "vim") {
    return "vim";
  }

  return defaultEditorSettings.keymap;
}

/**
 * Read one cookie value by name from document.cookie.
 *
 * Parameters:
 *   - cookieName: the cookie key to retrieve.
 *
 * Returns:
 *   - string: the decoded cookie value, or an empty string when the cookie is missing.
 */
function readCookieValue(cookieName) {
  const cookiePrefix = String(cookieName || "") + "=";
  const cookies = document.cookie ? document.cookie.split(";") : [];
  let index = 0;

  for (index = 0; index < cookies.length; index += 1) {
    const cookie = cookies[index].trim();

    if (cookie.startsWith(cookiePrefix)) {
      return decodeURIComponent(cookie.slice(cookiePrefix.length));
    }
  }

  return "";
}

/**
 * Parse a cookie-backed boolean setting from the string values used in the
 * editor settings cookies.
 *
 * Parameters:
 *   - cookieValue: the raw cookie value to interpret.
 *   - fallbackValue: the boolean to return when cookieValue is empty or invalid.
 *
 * Returns:
 *   - boolean: the parsed boolean setting value.
 */
function parseBooleanCookieValue(cookieValue, fallbackValue) {
  const normalizedValue = String(cookieValue || "").toLowerCase();

  if (normalizedValue === "1" || normalizedValue === "true") {
    return true;
  }

  if (normalizedValue === "0" || normalizedValue === "false") {
    return false;
  }

  return fallbackValue;
}

/**
 * Persist one editor setting into a long-lived cookie so the browser remembers
 * the user's preferences without any server-side storage.
 *
 * Parameters:
 *   - cookieName: the cookie key to write.
 *   - cookieValue: the value to persist.
 *
 * Returns:
 *   - none: document.cookie is updated in place.
 */
function writeCookieValue(cookieName, cookieValue) {
  document.cookie = String(cookieName || "") + "=" + encodeURIComponent(String(cookieValue || "")) + "; Max-Age=31536000; Path=/; SameSite=Lax";
}

/**
 * Reflect the current settings object into the settings menu controls.
 *
 * Parameters:
 *   - settings: the normalized editor settings that should be shown in the form controls.
 *
 * Returns:
 *   - none: the menu controls are updated in place.
 */
function applySettingsToControls(settings) {
  Array.from(editorKeymapRadios).forEach(syncKeymapRadioInput);
  editorLineWrappingInput.checked = settings.lineWrapping;
  editorLineNumbersInput.checked = settings.lineNumbers;
}

/**
 * Update one keymap radio input to match the currently active editor settings.
 *
 * Parameters:
 *   - radioInput: the radio element to synchronize with editorState.settings.
 *
 * Returns:
 *   - none: radioInput.checked is updated in place.
 */
function syncKeymapRadioInput(radioInput) {
  radioInput.checked = radioInput.value === editorState.settings.keymap;
}

/**
 * Apply a new settings object to the live editor, persist the cookies, and
 * update the in-memory state.
 *
 * Parameters:
 *   - nextSettings: the settings object requested by the UI controls.
 *
 * Returns:
 *   - none: the cookies, in-memory settings, and editor compartments are updated in place.
 */
function applyEditorSettings(nextSettings) {
  const normalizedSettings = normalizeEditorSettings(nextSettings);
  const previousKeymap = editorState.settings.keymap;
  const effects = [];

  if (!editorState.view) {
    return;
  }

  if (previousKeymap === "vim" && normalizedSettings.keymap !== "vim") {
    leaveVimMode();
  }

  editorState.settings = normalizedSettings;
  writeCookieValue(editorCookieNames.keymap, normalizedSettings.keymap);
  writeCookieValue(editorCookieNames.lineWrapping, normalizedSettings.lineWrapping ? "1" : "0");
  writeCookieValue(editorCookieNames.lineNumbers, normalizedSettings.lineNumbers ? "1" : "0");
  applySettingsToControls(normalizedSettings);

  effects.push(vimCompartment.reconfigure(createVimExtension(normalizedSettings.keymap)));
  effects.push(lineWrappingCompartment.reconfigure(createLineWrappingExtension(normalizedSettings.lineWrapping)));
  effects.push(lineNumbersCompartment.reconfigure(createLineNumberExtensions(normalizedSettings.lineNumbers)));

  editorState.view.dispatch({ effects: effects });

  if (editorState.bootstrap.renderMode === "codemirror" && editorState.previewView) {
    rebuildEditorCodeMirrorPreview();
  }

  focusEditor();
  refreshEditorLayout();
}

/**
 * Move the editor back into standard key handling after the user turns Vim
 * mode off.
 *
 * Parameters:
 *   - none: the function reads the current editor view from editorState.
 *
 * Returns:
 *   - none: Vim mode is exited when the CM5 compatibility layer is available.
 */
function leaveVimMode() {
  const codeMirrorCompatibilityLayer = getCompatCodeMirrorInstance();

  if (!codeMirrorCompatibilityLayer) {
    return;
  }

  Vim.leaveVimMode(codeMirrorCompatibilityLayer);
}

/**
 * Return the compatibility-layer CodeMirror instance exposed by the Vim plugin
 * for the active CM6 view.
 *
 * Parameters:
 *   - none: the function reads editorState.view.
 *
 * Returns:
 *   - object|null: the compatibility editor instance when available, otherwise null.
 */
function getCompatCodeMirrorInstance() {
  if (!editorState.view) {
    return null;
  }

  return getCM(editorState.view);
}

/**
 * Handle changes to the keymap radio inputs by applying the updated settings to
 * the editor and cookies.
 *
 * Parameters:
 *   - event: the DOM change event fired by a keymap radio input.
 *
 * Returns:
 *   - none: the selected keymap setting is applied when the changed radio becomes checked.
 */
function handleKeymapInputChange(event) {
  const changedInput = event.target;

  if (!changedInput || !changedInput.checked) {
    return;
  }

  applySettingsFromControls();
}

/**
 * Handle changes to the line-wrapping checkbox by applying the updated
 * settings to the editor and cookies.
 *
 * Parameters:
 *   - none: the function reads the shared settings form controls.
 *
 * Returns:
 *   - none: the live editor settings are updated in place.
 */
function handleLineWrappingInputChange() {
  applySettingsFromControls();
}

/**
 * Handle changes to the line-number checkbox by applying the updated settings
 * to the editor and cookies.
 *
 * Parameters:
 *   - none: the function reads the shared settings form controls.
 *
 * Returns:
 *   - none: the live editor settings are updated in place.
 */
function handleLineNumbersInputChange() {
  applySettingsFromControls();
}

/**
 * Read the current settings form control values and apply them to the editor.
 *
 * Parameters:
 *   - none: the function reads the settings inputs declared at module scope.
 *
 * Returns:
 *   - none: the normalized settings are persisted and applied to the editor.
 */
function applySettingsFromControls() {
  applyEditorSettings({
    keymap: getSelectedKeymapSetting(),
    lineWrapping: editorLineWrappingInput.checked,
    lineNumbers: editorLineNumbersInput.checked,
  });
}

/**
 * Read the currently selected keymap value from the settings radio inputs.
 *
 * Parameters:
 *   - none: the function reads the editorKeymapRadios node list.
 *
 * Returns:
 *   - string: the selected keymap identifier, or the default keymap when none is checked.
 */
function getSelectedKeymapSetting() {
  let index = 0;

  for (index = 0; index < editorKeymapRadios.length; index += 1) {
    if (editorKeymapRadios[index].checked) {
      return editorKeymapRadios[index].value;
    }
  }

  return defaultEditorSettings.keymap;
}

/**
 * Register Vim ex commands that should integrate with the Jotes editor shell,
 * such as :w for saving the current note.
 *
 * Parameters:
 *   - none: the function reads no external input.
 *
 * Returns:
 *   - none: the commands are registered once per page load.
 */
function registerVimCommands() {
  if (vimCommandsRegistered) {
    return;
  }

  Vim.defineEx("write", "w", handleVimWriteCommand);
  vimCommandsRegistered = true;
}

/**
 * Handle the Vim :w command by forwarding it to the standard immediate-save
 * flow used by the Jotes editor toolbar.
 *
 * Parameters:
 *   - none: the Vim integration does not require the ex-command callback arguments here.
 *
 * Returns:
 *   - Promise<void>: resolves after any save request completes.
 */
async function handleVimWriteCommand() {
  await saveCurrentNote();
}

/**
 * React to CodeMirror document updates so live preview refresh scheduling stays
 * in sync with the current editor document.
 *
 * Parameters:
 *   - update: the CodeMirror view update object emitted after editor transactions.
 *
 * Returns:
 *   - none: rendered preview refresh work is triggered only when the document content changes.
 */
function handleEditorViewUpdate(update) {
  if (!update.docChanged) {
    return;
  }

  if (editorState.bootstrap.renderMode === "codemirror") {
    if (editorState.previewView) {
      replaceReadOnlyPreviewContent(editorState.previewView, update.state.doc.toString());
    }
    return;
  }

  scheduleRenderedPreview(180);
}

/**
 * Return the current editor document as a plain JavaScript string.
 *
 * Parameters:
 *   - none: the function reads the active CodeMirror state from editorState.view.
 *
 * Returns:
 *   - string: the complete editor document.
 */
function getEditorValue() {
  if (!editorState.view) {
    return "";
  }

  return editorState.view.state.doc.toString();
}

/**
 * Focus the live CodeMirror editor so keyboard input returns to the plaintext
 * editing surface.
 *
 * Parameters:
 *   - none: the function reads editorState.view.
 *
 * Returns:
 *   - none: the editor receives keyboard focus when available.
 */
function focusEditor() {
  if (!editorState.view) {
    return;
  }

  editorState.view.focus();
}

/**
 * Ask CodeMirror to recompute its layout after the editor panel becomes
 * visible or its settings change.
 *
 * Parameters:
 *   - none: the function reads editorState.view.
 *
 * Returns:
 *   - none: a layout measurement is requested when the editor exists.
 */
function refreshEditorLayout() {
  if (!editorState.view) {
    return;
  }

  editorState.view.requestMeasure();
}

/**
 * Ask the read-only preview CodeMirror instance to recompute its layout after
 * the rendered panel becomes visible.
 *
 * Parameters:
 *   - none: the function reads editorState.previewView.
 *
 * Returns:
 *   - none: a layout measurement is requested when the preview exists.
 */
function refreshEditorPreviewLayout() {
  if (!editorState.previewView) {
    return;
  }

  editorState.previewView.requestMeasure();
}

/**
 * Create the editor page's read-only CodeMirror preview for non-Markdown and
 * non-Org notes the first time the rendered tab needs it.
 *
 * Parameters:
 *   - none: the function reads the current editor bootstrap data, current editor settings, and rendered preview container.
 *
 * Returns:
 *   - none: editorState.previewView is created once when the current note uses the local CodeMirror preview mode.
 */
function initializeEditorCodeMirrorPreview() {
  const previewContent = editorState.view ? getEditorValue() : editorState.bootstrap.initialContent;

  if (editorState.bootstrap.renderMode !== "codemirror" || editorState.previewView || !editorRenderOutput) {
    return;
  }

  editorState.previewView = createReadOnlyPreviewView(
    editorRenderOutput,
    previewContent,
    editorState.bootstrap.filePath,
    editorState.bootstrap.mimeType,
    createReadOnlyPreviewOptions(editorState.settings, editorRenderOutput.getAttribute("aria-label") || "Read-only note preview"),
  );
}

/**
 * Rebuild the editor page's read-only CodeMirror preview so line-number and
 * wrapping setting changes take effect immediately.
 *
 * Parameters:
 *   - none: the function reads the current preview instance, editor settings, and editor content from shared state.
 *
 * Returns:
 *   - none: the preview is recreated in place when the current note uses the local CodeMirror preview mode.
 */
function rebuildEditorCodeMirrorPreview() {
  if (editorState.bootstrap.renderMode !== "codemirror" || !editorRenderOutput) {
    return;
  }

  if (editorState.previewView) {
    editorState.previewView.destroy();
    editorState.previewView = null;
  }

  initializeEditorCodeMirrorPreview();
  window.requestAnimationFrame(refreshEditorPreviewLayout);
}

/**
 * Toggle the editor between plaintext editing and rendered preview modes.
 *
 * Parameters:
 *   - none: the handler reads the current active mode from editorRoot dataset state.
 *
 * Returns:
 *   - none: the visible panel, toggle label, and toggle color are updated in place.
 */
function handleEditorViewToggleButtonClick() {
  const nextMode = editorRoot.dataset.activeMode === "rendered" ? "plain" : "rendered";

  setMode(nextMode);

  if (nextMode === "rendered") {
    requestRenderedPreview();
    return;
  }

  focusEditor();
}

/**
 * Handle clicks on the save button by forwarding them to the shared
 * immediate-save flow.
 *
 * Parameters:
 *   - none: the handler reads no event-specific data.
 *
 * Returns:
 *   - Promise<void>: resolves after any save request completes.
 */
async function handleSaveButtonClick() {
  await saveCurrentNote();
}

/**
 * Update the visible editor mode and synchronize the single toggle button's
 * label, color state, and accessibility metadata.
 *
 * Parameters:
 *   - mode: either "plain" or "rendered" depending on the desired active panel.
 *
 * Returns:
 *   - none: the DOM state is updated in place.
 */
function setMode(mode) {
  const isPlain = mode === "plain";
  const nextToggleLabel = isPlain ? "Rendered" : "Plaintext";
  const nextToggleDescription = isPlain ? "Show rendered preview" : "Show plaintext editor";

  editorRoot.dataset.activeMode = mode;
  editorViewToggleButton.classList.toggle("is-plain", isPlain);
  editorViewToggleButton.classList.toggle("is-rendered", !isPlain);
  editorViewToggleButton.textContent = nextToggleLabel;
  editorViewToggleButton.setAttribute("aria-pressed", String(!isPlain));
  editorViewToggleButton.setAttribute("aria-label", nextToggleDescription);
  editorPlainPanel.hidden = !isPlain;
  editorRenderedPanel.hidden = isPlain;
  editorPlainPanel.classList.toggle("is-active", isPlain);
  editorRenderedPanel.classList.toggle("is-active", !isPlain);

  if (isPlain) {
    window.requestAnimationFrame(refreshEditorLayout);
  }
}

/**
 * Toggle the save button's busy presentation so users get immediate feedback
 * while one save request is running.
 *
 * Parameters:
 *   - isBusy: true when a save request is in flight, otherwise false.
 *
 * Returns:
 *   - none: the save button label, disabled state, and aria-busy flag are updated in place.
 */
function setEditorSaveButtonBusy(isBusy) {
  editorSaveButton.disabled = isBusy;
  editorSaveButton.textContent = isBusy ? "Saving…" : defaultEditorSaveButtonLabel;
  editorSaveButton.setAttribute("aria-busy", String(isBusy));
}

/**
 * Forward one editor notification to the shared sitewide toast helper when it
 * is available on the current page.
 *
 * Parameters:
 *   - kind: the toast variant string expected by the shared notification helper, such as success or error.
 *   - message: the user-facing message that should appear inside the toast.
 *
 * Returns:
 *   - none: a shared toast is shown when the helper exists and the message is not empty.
 */
function showEditorToastNotification(kind, message) {
  if (!message || typeof window.jotesShowToastNotification !== "function") {
    return;
  }

  window.jotesShowToastNotification({
    kind,
    message,
    durationMs: kind === "error" ? 5600 : 3200,
  });
}

/**
 * Normalize one successful save response message into the friendlier toast copy
 * shown to editor users.
 *
 * Parameters:
 *   - message: the optional success text returned by the save API.
 *
 * Returns:
 *   - string: the preferred success toast text, defaulting to "All changes saved." when the API response is generic.
 */
function buildEditorSaveSuccessMessage(message) {
  if (message && message !== "Saved") {
    return message;
  }

  return "All changes saved.";
}

/**
 * Report whether the current editor document differs from the last content
 * confirmed by a successful save response.
 *
 * Parameters:
 *   - none: the function reads the live editor document and cached last-saved text.
 *
 * Returns:
 *   - boolean: true when unsaved changes exist, otherwise false.
 */
function getIsDirty() {
  return getEditorValue() !== editorState.lastSavedContent;
}

/**
 * Start or replace the debounce timer used for live rendered preview refreshes.
 *
 * Parameters:
 *   - delayMs: the number of milliseconds to wait before requesting a fresh preview.
 *
 * Returns:
 *   - none: the previous timer is cleared and the new timer is scheduled.
 */
function scheduleRenderedPreview(delayMs) {
  if (editorState.renderTimer) {
    clearTimeout(editorState.renderTimer);
  }

  editorState.renderTimer = window.setTimeout(requestRenderedPreview, delayMs);
}

/**
 * Read a JSON API response body and either return the parsed payload or throw
 * a structured error with a useful fallback message.
 *
 * Parameters:
 *   - response: the Fetch API response expected to contain JSON.
 *   - fallbackMessage: the message to use when the response body is missing or malformed.
 *
 * Returns:
 *   - Promise<object>: resolves with the parsed JSON payload or rejects with an Error.
 */
async function readEditorJSON(response, fallbackMessage) {
  let payload;

  try {
    payload = await response.json();
  } catch (error) {
    if (response.ok) {
      throw error;
    }

    throw new Error(fallbackMessage);
  }

  if (!response.ok) {
    throw new Error(payload.error || fallbackMessage);
  }

  return payload;
}

/**
 * Refresh the rendered preview for the current note using either the local
 * read-only CodeMirror preview path or the server-rendered Markdown/Org path.
 *
 * Parameters:
 *   - none: the function reads the active editor document and bootstrap paths from shared state.
 *
 * Returns:
 *   - Promise<void>: resolves after the preview has been updated or an error message has been shown.
 */
async function requestRenderedPreview() {
  const requestBody = {
    path: editorState.bootstrap.filePath,
    content: getEditorValue(),
  };
  let requestId;
  let response;
  let payload;

  if (editorState.bootstrap.renderMode === "codemirror") {
    initializeEditorCodeMirrorPreview();

    if (!editorState.previewView) {
      return;
    }

    replaceReadOnlyPreviewContent(editorState.previewView, requestBody.content);
    window.requestAnimationFrame(refreshEditorPreviewLayout);
    return;
  }

  requestId = editorState.renderRequestId + 1;
  editorState.renderRequestId = requestId;

  try {
    response = await fetch(editorState.bootstrap.renderUrl, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "X-Jotes-Editor": "1",
      },
      body: JSON.stringify(requestBody),
    });

    payload = await readEditorJSON(response, "Preview is temporarily unavailable.");

    if (requestId < editorState.renderRequestId) {
      return;
    }

    applyRenderedHTML(payload.html || "");
  } catch (error) {
    if (requestId < editorState.renderRequestId) {
      return;
    }

    renderPreviewMessage("render-preview-error", error.message || "Preview is temporarily unavailable.");
  }
}

/**
 * Replace the rendered preview container contents with the HTML fragment
 * returned by the server and reapply the shared rendered-code-block
 * enhancements used by standalone note previews.
 *
 * Parameters:
 *   - html: the server-rendered HTML fragment to display in the preview panel.
 *
 * Returns:
 *   - none: the preview container markup is replaced in place and any rendered code blocks or tables are enhanced when the shared helper is available.
 */
function applyRenderedHTML(html) {
  editorRenderOutput.innerHTML = html;

  if (typeof window.jotesEnhanceRenderedPreviewContent === "function") {
    window.jotesEnhanceRenderedPreviewContent(editorRenderOutput);
  }
}

/**
 * Replace the rendered preview container contents with a plain-text status
 * message, typically after a failed preview request.
 *
 * Parameters:
 *   - className: the CSS class applied to the generated message element.
 *   - message: the user-facing message to render in the preview panel.
 *
 * Returns:
 *   - none: the preview container is emptied and repopulated with one paragraph.
 */
function renderPreviewMessage(className, message) {
  const paragraph = document.createElement("p");

  paragraph.className = className;
  paragraph.textContent = message;
  editorRenderOutput.innerHTML = "";
  editorRenderOutput.appendChild(paragraph);
}

/**
 * Persist the current editor document to the server when unsaved changes exist,
 * update the revision token on success, and surface the outcome through shared
 * toast notifications.
 *
 * Parameters:
 *   - none: the function reads the active document, path, and revision from shared state.
 *
 * Returns:
 *   - Promise<void>: resolves after the save request completes and any follow-up preview refresh has been requested.
 */
async function saveCurrentNote() {
  const currentContent = getEditorValue();
  const requestBody = {
    path: editorState.bootstrap.filePath,
    content: currentContent,
    revision: editorState.currentRevision,
  };
  let response;
  let payload;

  if (editorState.saveInFlight) {
    return;
  }

  if (!getIsDirty()) {
    showEditorToastNotification("success", "All changes saved.");
    return;
  }

  editorState.saveInFlight = true;
  setEditorSaveButtonBusy(true);

  try {
    response = await fetch(editorState.bootstrap.saveUrl, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "X-Jotes-Editor": "1",
      },
      body: JSON.stringify(requestBody),
    });

    payload = await readEditorJSON(response, "Save failed");
    editorState.lastSavedContent = currentContent;
    editorState.currentRevision = payload.revision || editorState.currentRevision;
    editorRoot.dataset.revision = editorState.currentRevision;
    showEditorToastNotification("success", buildEditorSaveSuccessMessage(payload.message));

    if (editorRoot.dataset.activeMode === "rendered") {
      await requestRenderedPreview();
    }
  } catch (error) {
    showEditorToastNotification("error", error.message || "Save failed");
  } finally {
    editorState.saveInFlight = false;
    setEditorSaveButtonBusy(false);
  }
}

/**
 * Intercept the standard save keyboard shortcut so the editor can persist the
 * current note without leaving the page.
 *
 * Parameters:
 *   - event: the KeyboardEvent fired by the document.
 *
 * Returns:
 *   - none: the browser default action is suppressed when the shortcut matches.
 */
function handleSaveShortcut(event) {
  if (!(event.ctrlKey || event.metaKey)) {
    return;
  }

  if (String(event.key).toLowerCase() !== "s") {
    return;
  }

  event.preventDefault();
  saveCurrentNote();
}

/**
 * Warn the user before navigating away while the editor still contains unsaved
 * changes or an active save request.
 *
 * Parameters:
 *   - event: the BeforeUnloadEvent raised by the browser.
 *
 * Returns:
 *   - none: the event is mutated in place to trigger the browser confirmation dialog when needed.
 */
function handleBeforeUnload(event) {
  if (!getIsDirty() && !editorState.saveInFlight) {
    return;
  }

  event.preventDefault();
  event.returnValue = "";
}

initializeStandaloneCodeMirrorPreviews();
initializeEditorPage();
