import * as pdfjsLib from "pdfjs-dist/legacy/build/pdf.mjs";

const previewRootSelector = '[data-pdf-preview-root="1"]';
const statusSelector = "[data-pdf-preview-status]";
const metaSelector = "[data-pdf-preview-meta]";
const pagesSelector = "[data-pdf-preview-pages]";
const defaultWorkerSourcePath = "/static/pdfjs/pdf.worker.mjs";

/**
 * configurePDFWorker points PDF.js at the locally bundled worker script so Jotes can
 * render PDFs entirely offline without any CDN or blob-worker dependency.
 *
 * Parameters:
 *   - workerUrl: the same-origin static worker asset URL that PDF.js should load.
 *
 * Returns:
 *   - void: PDF.js global worker configuration is updated in place.
 */
function configurePDFWorker(workerUrl) {
  pdfjsLib.GlobalWorkerOptions.workerSrc = workerUrl || defaultWorkerSourcePath;
}

/**
 * readPreviewConfig extracts the static configuration embedded on one PDF preview root.
 *
 * Parameters:
 *   - root: the DOM element representing one PDF preview surface on the preview page.
 *
 * Returns:
 *   - Object: the normalized preview configuration containing the PDF URL, file name,
 *     worker URL, character-map directory, and standard-font directory for the current preview.
 */
function readPreviewConfig(root) {
  return {
    url: root.dataset.pdfUrl || "",
    fileName: root.dataset.pdfFileName || "PDF document",
    workerUrl: root.dataset.pdfWorkerUrl || defaultWorkerSourcePath,
    cMapUrl: root.dataset.pdfCmapUrl || "",
    standardFontDataUrl: root.dataset.pdfStandardFontUrl || "",
  };
}

/**
 * updateStatusMessage replaces the primary status text shown above one PDF preview.
 *
 * Parameters:
 *   - root: the DOM element representing one PDF preview surface.
 *   - message: the user-facing status message that should be displayed.
 *
 * Returns:
 *   - void: the preview status element is updated when present.
 */
function updateStatusMessage(root, message) {
  const status = root.querySelector(statusSelector);
  if (status) {
    status.textContent = message;
  }
}

/**
 * updateMetaMessage updates the secondary metadata line for one PDF preview and can
 * optionally hide it when no metadata should be shown.
 *
 * Parameters:
 *   - root: the DOM element representing one PDF preview surface.
 *   - message: the metadata text to display when show is true.
 *   - show: true to reveal the metadata element, or false to hide it.
 *
 * Returns:
 *   - void: the preview metadata element is updated when present.
 */
function updateMetaMessage(root, message, show) {
  const meta = root.querySelector(metaSelector);
  if (!meta) {
    return;
  }

  meta.textContent = message;
  meta.hidden = !show;
}

/**
 * clearPagesContainer removes all previously rendered page surfaces from one preview
 * so a fresh render starts from an empty state.
 *
 * Parameters:
 *   - root: the DOM element representing one PDF preview surface.
 *
 * Returns:
 *   - HTMLElement|null: the pages container when it exists, otherwise null.
 */
function clearPagesContainer(root) {
  const pages = root.querySelector(pagesSelector);
  if (!pages) {
    return null;
  }

  pages.replaceChildren();
  return pages;
}

/**
 * buildLoadingTask creates one PDF.js loading task configured entirely against local
 * Jotes assets so preview rendering works offline.
 *
 * Parameters:
 *   - config: the preview configuration containing the document URL and local PDF.js
 *     support-asset paths.
 *
 * Returns:
 *   - Object: the PDF.js loading task returned by pdfjsLib.getDocument.
 */
function buildLoadingTask(config) {
  return pdfjsLib.getDocument({
    url: config.url,
    cMapUrl: config.cMapUrl,
    cMapPacked: true,
    standardFontDataUrl: config.standardFontDataUrl,
  });
}

/**
 * createPageSurface creates the DOM nodes needed to host one rendered PDF page,
 * including both the raster canvas and the selectable text-layer overlay used
 * for highlighting and copying document text.
 *
 * Parameters:
 *   - pagesContainer: the preview container that should receive the new page surface.
 *   - pageNumber: the 1-based PDF page number represented by the new surface.
 *
 * Returns:
 *   - Object: {canvas, pageContent, textLayer} where canvas receives the page bitmap,
 *     pageContent establishes the positioned overlay stack, and textLayer hosts selectable text spans.
 */
function createPageSurface(pagesContainer, pageNumber) {
  const pageSection = document.createElement("section");
  pageSection.className = "pdf-preview-page";

  const pageLabel = document.createElement("p");
  pageLabel.className = "pdf-preview-page-label";
  pageLabel.textContent = `Page ${pageNumber}`;

  const canvasShell = document.createElement("div");
  canvasShell.className = "pdf-preview-canvas-shell";

  const pageContent = document.createElement("div");
  pageContent.className = "pdf-preview-page-content";

  const canvas = document.createElement("canvas");
  canvas.setAttribute("aria-label", `Rendered PDF page ${pageNumber}`);

  const textLayer = document.createElement("div");
  textLayer.className = "pdf-preview-text-layer textLayer";
  textLayer.setAttribute("aria-hidden", "true");

  pageContent.appendChild(canvas);
  pageContent.appendChild(textLayer);
  canvasShell.appendChild(pageContent);
  pageSection.appendChild(pageLabel);
  pageSection.appendChild(canvasShell);
  pagesContainer.appendChild(pageSection);

  return {
    canvas,
    pageContent,
    textLayer,
  };
}

/**
 * computeRenderScale determines a fit-to-width scale for one PDF page based on the
 * current preview container width while preventing nonsensical zero or negative values.
 *
 * Parameters:
 *   - page: the PDF.js page proxy that will be rendered.
 *   - pagesContainer: the DOM container whose width constrains the preview layout.
 *
 * Returns:
 *   - number: the viewport scale that best fits the current preview width.
 */
function computeRenderScale(page, pagesContainer) {
  const unscaledViewport = page.getViewport({ scale: 1 });
  const availableWidth = Math.max((pagesContainer.clientWidth || 0) - 32, 240);
  const computedScale = availableWidth / unscaledViewport.width;

  if (!Number.isFinite(computedScale) || computedScale <= 0) {
    return 1;
  }

  return computedScale;
}

/**
 * applyPageViewportDimensions synchronizes the shared page-content wrapper, its
 * raster canvas, and its text-layer overlay to the same viewport dimensions so
 * PDF.js text spans align exactly over the rendered page pixels.
 *
 * Parameters:
 *   - pageSurface: the {canvas, pageContent, textLayer} object returned by createPageSurface.
 *   - viewport: the PDF.js viewport used for both canvas rendering and text-layer layout.
 *   - outputScale: the current device-pixel-ratio scale applied to the backing canvas.
 *   - scale: the PDF.js viewport scale used for the current fit-to-width page render; this must also be exposed through --total-scale-factor so PDF.js text metrics stay aligned with the canvas.
 *
 * Returns:
 *   - void: the supplied DOM elements are resized in place to match viewport and the text layer receives the correct PDF.js scale variable.
 */
function applyPageViewportDimensions(pageSurface, viewport, outputScale, scale) {
  const cssWidth = `${viewport.width}px`;
  const cssHeight = `${viewport.height}px`;
  const renderWidth = Math.floor(viewport.width * outputScale);
  const renderHeight = Math.floor(viewport.height * outputScale);

  pageSurface.canvas.width = renderWidth;
  pageSurface.canvas.height = renderHeight;
  pageSurface.canvas.style.width = cssWidth;
  pageSurface.canvas.style.height = cssHeight;
  pageSurface.pageContent.style.width = cssWidth;
  pageSurface.pageContent.style.height = cssHeight;
  pageSurface.pageContent.style.setProperty("--total-scale-factor", String(scale));
  pageSurface.textLayer.style.width = cssWidth;
  pageSurface.textLayer.style.height = cssHeight;
  pageSurface.textLayer.replaceChildren();
}

/**
 * renderPageTextLayer builds the selectable PDF.js text layer for one rendered
 * page so document text can be highlighted and copied from the preview.
 *
 * Parameters:
 *   - page: the PDF.js page proxy whose text content should be exposed.
 *   - textLayerContainer: the positioned DOM element that should host the rendered text spans.
 *   - viewport: the same PDF.js viewport used for the matching canvas render.
 *
 * Returns:
 *   - Promise<void>: resolves after the text layer has finished rendering into textLayerContainer.
 */
async function renderPageTextLayer(page, textLayerContainer, viewport) {
  const textLayer = new pdfjsLib.TextLayer({
    textContentSource: page.streamTextContent(),
    container: textLayerContainer,
    viewport,
  });

  await textLayer.render();
}

/**
 * renderPageSurface rasterizes one PDF page into its target canvas and renders
 * the matching selectable text layer above it using the same viewport.
 *
 * Parameters:
 *   - page: the PDF.js page proxy that should be drawn.
 *   - pageSurface: the {canvas, pageContent, textLayer} object returned by createPageSurface.
 *   - pagesContainer: the DOM container whose width constrains the preview layout.
 *
 * Returns:
 *   - Promise<void>: resolves after both the canvas pixels and the copyable text layer have finished rendering.
 */
async function renderPageSurface(page, pageSurface, pagesContainer) {
  const scale = computeRenderScale(page, pagesContainer);
  const viewport = page.getViewport({ scale });
  const outputScale = window.devicePixelRatio || 1;
  const canvasContext = pageSurface.canvas.getContext("2d");

  applyPageViewportDimensions(pageSurface, viewport, outputScale, scale);

  if (!canvasContext) {
    throw new Error("Jotes could not create a 2D canvas context for this PDF preview.");
  }

  const transform = outputScale !== 1 ? [outputScale, 0, 0, outputScale, 0, 0] : null;
  const renderTask = page.render({
    canvasContext,
    transform,
    viewport,
    background: "rgb(255, 255, 255)",
    annotationMode: pdfjsLib.AnnotationMode.ENABLE,
    intent: "display",
  });

  await Promise.all([
    renderTask.promise,
    renderPageTextLayer(page, pageSurface.textLayer, viewport),
  ]);
}

/**
 * showPreviewError replaces the current preview content with one clear non-editable
 * error message when a PDF cannot be rendered.
 *
 * Parameters:
 *   - root: the DOM element representing one PDF preview surface.
 *   - message: the user-facing failure explanation to display.
 *
 * Returns:
 *   - void: the preview content and status region are updated in place.
 */
function showPreviewError(root, message) {
  updateStatusMessage(root, "PDF preview unavailable");
  updateMetaMessage(root, "", false);

  const pages = clearPagesContainer(root);
  if (!pages) {
    return;
  }

  const errorMessage = document.createElement("p");
  errorMessage.className = "pdf-preview-error";
  errorMessage.textContent = message;
  pages.appendChild(errorMessage);
}

/**
 * renderPreviewDocument downloads and renders every page of one PDF document into
 * the supplied preview root.
 *
 * Parameters:
 *   - root: the DOM element representing one PDF preview surface.
 *   - config: the normalized preview configuration for the current PDF document.
 *
 * Returns:
 *   - Promise<void>: resolves after all pages have rendered or rejects on the first fatal error.
 */
async function renderPreviewDocument(root, config) {
  const pages = clearPagesContainer(root);
  if (!pages) {
    throw new Error("Jotes could not find the PDF page container on this preview page.");
  }

  updateStatusMessage(root, "Loading PDF preview…");
  updateMetaMessage(root, "", false);

  const loadingTask = buildLoadingTask(config);
  const pdfDocument = await loadingTask.promise;

  if (!pdfDocument || pdfDocument.numPages < 1) {
    const emptyMessage = document.createElement("p");
    emptyMessage.className = "pdf-preview-empty";
    emptyMessage.textContent = "This PDF does not contain any renderable pages.";
    pages.appendChild(emptyMessage);
    updateStatusMessage(root, "No previewable PDF pages found");
    updateMetaMessage(root, "", false);
    return;
  }

  updateMetaMessage(root, `${pdfDocument.numPages} page${pdfDocument.numPages === 1 ? "" : "s"}`, true);

  for (let pageNumber = 1; pageNumber <= pdfDocument.numPages; pageNumber += 1) {
    updateStatusMessage(root, `Rendering page ${pageNumber} of ${pdfDocument.numPages}…`);

    const page = await pdfDocument.getPage(pageNumber);
    const pageSurface = createPageSurface(pages, pageNumber);
    await renderPageSurface(page, pageSurface, pages);
  }

  updateStatusMessage(root, `Preview ready for ${config.fileName}`);
}

/**
 * initializePreviewRoot validates one preview root and either renders its PDF or
 * reports a clear configuration/runtime error to the user.
 *
 * Parameters:
 *   - root: the DOM element representing one PDF preview surface.
 *
 * Returns:
 *   - Promise<void>: resolves after the preview has either rendered or displayed an error state.
 */
async function initializePreviewRoot(root) {
  const config = readPreviewConfig(root);
  if (!config.url) {
    showPreviewError(root, "Jotes could not determine which PDF file to preview.");
    return;
  }

  try {
    await renderPreviewDocument(root, config);
  } catch (error) {
    const message = error instanceof Error && error.message
      ? error.message
      : "Jotes could not render this PDF file right now.";
    showPreviewError(root, message);
  }
}

/**
 * initializePDFPreviewPage finds every PDF preview root on the current page and
 * renders them one after another so the preview screen stays read-only and offline.
 *
 * Parameters:
 *   - none: the function discovers preview roots directly from the current document.
 *
 * Returns:
 *   - Promise<void>: resolves after all discovered PDF previews have finished initializing.
 */
async function initializePDFPreviewPage() {
  const previewRoots = document.querySelectorAll(previewRootSelector);
  const firstPreviewRoot = previewRoots.length > 0 ? previewRoots[0] : null;
  const firstPreviewConfig = firstPreviewRoot ? readPreviewConfig(firstPreviewRoot) : null;
  configurePDFWorker(firstPreviewConfig ? firstPreviewConfig.workerUrl : defaultWorkerSourcePath);

  for (const root of previewRoots) {
    await initializePreviewRoot(root);
  }
}

/**
 * logBootstrapError records one unexpected top-level PDF preview bootstrap failure
 * without interrupting the rest of the already rendered page.
 *
 * Parameters:
 *   - error: the failure thrown while initializing PDF preview roots.
 *
 * Returns:
 *   - void: the error is written to the browser console for debugging.
 */
function logBootstrapError(error) {
  console.error("Jotes PDF preview bootstrap failed", error);
}

/**
 * runPDFPreviewBootstrap executes the PDF preview bootstrap routine and logs any
 * unexpected top-level failure without interrupting the rest of the page.
 *
 * Parameters:
 *   - none: the function reads from the current DOM and browser console only.
 *
 * Returns:
 *   - void: asynchronous initialization is launched and any fatal error is logged.
 */
function runPDFPreviewBootstrap() {
  void initializePDFPreviewPage().catch(logBootstrapError);
}

if (document.readyState === "loading") {
  document.addEventListener("DOMContentLoaded", runPDFPreviewBootstrap, { once: true });
} else {
  runPDFPreviewBootstrap();
}
