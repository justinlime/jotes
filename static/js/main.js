/**
 * main.js – Jotes UI.
 *
 * Search is delegated to search-worker.js so backend requests, cancellation,
 * and result handling stay off the main UI event path.
 */

(function () {
  "use strict";

  // ------------------------------------------------------------------ //
  // DOM helpers                                                          //
  // ------------------------------------------------------------------ //

  const searchInput = document.getElementById("search-input");
  const searchResults = document.getElementById("search-results");
  const searchSettingsBtn = document.getElementById("search-settings-btn");
  const searchSettingsMenu = document.getElementById("search-settings-menu");
  const radioByName = document.getElementById("search-by-name");
  const radioByAuto = document.getElementById("search-by-auto");
  const radioByContent = document.getElementById("search-by-content");
  const modalSettingsDetails = Array.from(document.querySelectorAll(".dir-settings, .editor-settings"));
  const toastBootstrapNodes = Array.from(document.querySelectorAll("[data-jotes-toast-message]"));
  var toastRegion = null;
  var activeToastElement = null;
  var activeToastDismissTimer = 0;
  var activeToastRemovalTimer = 0;
  var activeConfirmationOverlay = null;
  var activeConfirmationResolve = null;
  var activeConfirmationPreviouslyFocusedElement = null;
  var activeConfirmationKeydownHandler = null;

  /**
   * Normalize one requested toast variant so the UI only renders supported
   * success and error styles.
   *
   * Parameters:
   *   - kind: the requested toast variant string, typically supplied by callers or data attributes.
   *
   * Returns:
   *   - string: "error" when the caller explicitly requests that variant, otherwise "success".
   */
  function normalizeToastKind(kind) {
    return kind === "error" ? "error" : "success";
  }

  /**
   * Create the shared toast region on demand and return the live DOM node used
   * for all transient notifications, reusing an already-created region when a
   * page-specific script inserted one before the shared helper was first used.
   *
   * Parameters:
   *   - none: the function reads and updates the module-scoped toastRegion cache.
   *
   * Returns:
   *   - HTMLElement|null: the fixed-position toast container, or null when the document body is unavailable.
   */
  function getToastRegion() {
    var existingRegion = null;

    if (toastRegion && document.body && document.body.contains(toastRegion)) {
      return toastRegion;
    }

    if (!document.body) {
      return null;
    }

    existingRegion = document.querySelector(".jotes-toast-region");
    if (existingRegion) {
      toastRegion = existingRegion;
      return toastRegion;
    }

    toastRegion = document.createElement("div");
    toastRegion.className = "jotes-toast-region";
    toastRegion.setAttribute("aria-live", "polite");
    toastRegion.setAttribute("aria-atomic", "true");
    document.body.appendChild(toastRegion);
    return toastRegion;
  }

  /**
   * Dismiss the currently visible toast, clearing any pending timers and
   * removing the notification after its exit animation finishes.
   *
   * Parameters:
   *   - none: the function reads and updates the module-scoped active toast references.
   *
   * Returns:
   *   - none: the active toast is hidden immediately and detached from the DOM shortly after.
   */
  function hideActiveToastNotification() {
    var toastToHide = activeToastElement;

    clearTimeout(activeToastDismissTimer);
    clearTimeout(activeToastRemovalTimer);
    activeToastDismissTimer = 0;
    activeToastRemovalTimer = 0;

    if (!toastToHide) {
      return;
    }

    activeToastElement = null;
    toastToHide.classList.remove("is-visible");
    activeToastRemovalTimer = window.setTimeout(function () {
      if (toastToHide.parentNode) {
        toastToHide.parentNode.removeChild(toastToHide);
      }
    }, 200);
  }

  /**
   * Render one transient toast notification using the shared mobile-friendly
   * and desktop-friendly presentation.
   *
   * Parameters:
   *   - options: object shaped like {kind, message, durationMs} describing the toast variant, visible text, and optional auto-dismiss delay.
   *
   * Returns:
   *   - HTMLElement|null: the created toast element, or null when no message can be displayed.
   */
  function showToastNotification(options) {
    var region = getToastRegion();
    var kind = normalizeToastKind(options && options.kind);
    var message = options && typeof options.message === "string" ? options.message.trim() : "";
    var durationMs = options && typeof options.durationMs === "number" && options.durationMs > 0 ? options.durationMs : kind === "error" ? 5600 : 3200;
    var toast = null;

    if (!region || !message) {
      return null;
    }

    hideActiveToastNotification();

    toast = document.createElement("div");
    toast.className = "jotes-toast jotes-toast--" + kind;
    toast.setAttribute("role", kind === "error" ? "alert" : "status");
    toast.textContent = message;
    toast.addEventListener("click", hideActiveToastNotification);
    region.appendChild(toast);
    activeToastElement = toast;

    window.requestAnimationFrame(function () {
      if (activeToastElement === toast) {
        toast.classList.add("is-visible");
      }
    });

    activeToastDismissTimer = window.setTimeout(hideActiveToastNotification, durationMs);
    return toast;
  }

  /**
   * Render one success toast using the shared notification styling that other
   * pages can reuse through the exposed window helper.
   *
   * Parameters:
   *   - message: the user-facing success text that should be displayed.
   *
   * Returns:
   *   - HTMLElement|null: the created toast element, or null when no message was provided.
   */
  function showSuccessToastNotification(message) {
    return showToastNotification({
      kind: "success",
      message: message,
    });
  }

  /**
   * Expose the shared toast helpers globally and replay any server-rendered
   * toast bootstrap elements already present in the current document.
   *
   * Parameters:
   *   - none: the function reads the module-scoped bootstrap node list and attaches helpers to window.
   *
   * Returns:
   *   - none: global toast helpers are registered and bootstrap notices are shown once.
   */
  function initializeToastNotifications() {
    window.jotesShowToastNotification = showToastNotification;
    window.jotesShowSuccessToastNotification = showSuccessToastNotification;

    toastBootstrapNodes.forEach(function (node) {
      showToastNotification({
        kind: node.getAttribute("data-jotes-toast-kind") || "success",
        message: node.getAttribute("data-jotes-toast-message") || "",
      });
      node.remove();
    });
  }

  /**
   * Normalize one requested confirmation-dialog variant so the shared modal only
   * renders supported button treatments.
   *
   * Parameters:
   *   - kind: the requested confirmation variant string, typically supplied by callers or data attributes.
   *
   * Returns:
   *   - string: "danger" when the caller explicitly requests that variant, otherwise "success".
   */
  function normalizeConfirmationKind(kind) {
    return kind === "danger" ? "danger" : "success";
  }

  /**
   * Close the active confirmation dialog, restore focus, and resolve the
   * caller's pending Promise with the user's choice.
   *
   * Parameters:
   *   - confirmed: true when the user accepted the action, otherwise false.
   *
   * Returns:
   *   - none: the current confirmation dialog is removed from the DOM and its Promise resolves once.
   */
  function closeActiveConfirmationDialog(confirmed) {
    var overlay = activeConfirmationOverlay;
    var resolveDialog = activeConfirmationResolve;
    var previouslyFocusedElement = activeConfirmationPreviouslyFocusedElement;

    if (activeConfirmationKeydownHandler) {
      document.removeEventListener("keydown", activeConfirmationKeydownHandler);
    }

    activeConfirmationOverlay = null;
    activeConfirmationResolve = null;
    activeConfirmationPreviouslyFocusedElement = null;
    activeConfirmationKeydownHandler = null;

    if (overlay && overlay.parentNode) {
      overlay.parentNode.removeChild(overlay);
    }

    if (previouslyFocusedElement && typeof previouslyFocusedElement.focus === "function") {
      previouslyFocusedElement.focus();
    }

    if (resolveDialog) {
      resolveDialog(Boolean(confirmed));
    }
  }

  /**
   * Show the shared confirmation dialog used for saves and admin account
   * actions, then resolve with the user's decision.
   *
   * Parameters:
   *   - options: object shaped like {title, message, confirmLabel, cancelLabel, kind} describing the dialog copy and confirm-button treatment.
   *
   * Returns:
   *   - Promise<boolean>: resolves true when the user confirms the action, otherwise false.
   */
  function showConfirmationDialog(options) {
    var title = options && typeof options.title === "string" && options.title.trim() ? options.title.trim() : "Confirm action";
    var message = options && typeof options.message === "string" && options.message.trim() ? options.message.trim() : "Are you sure you want to continue?";
    var confirmLabel = options && typeof options.confirmLabel === "string" && options.confirmLabel.trim() ? options.confirmLabel.trim() : "Confirm";
    var cancelLabel = options && typeof options.cancelLabel === "string" && options.cancelLabel.trim() ? options.cancelLabel.trim() : "Cancel";
    var kind = normalizeConfirmationKind(options && options.kind);
    var overlay = document.createElement("div");
    var dialog = document.createElement("div");
    var heading = document.createElement("h2");
    var copy = document.createElement("p");
    var actions = document.createElement("div");
    var cancelButton = document.createElement("button");
    var confirmButton = document.createElement("button");
    var titleId = "jotes-confirmation-title";
    var messageId = "jotes-confirmation-copy";

    closeActiveConfirmationDialog(false);

    overlay.className = "jotes-confirmation-overlay";
    dialog.className = "jotes-confirmation-dialog";
    dialog.setAttribute("role", "dialog");
    dialog.setAttribute("aria-modal", "true");
    dialog.setAttribute("aria-labelledby", titleId);
    dialog.setAttribute("aria-describedby", messageId);
    heading.className = "jotes-confirmation-title";
    heading.id = titleId;
    heading.textContent = title;
    copy.className = "jotes-confirmation-copy";
    copy.id = messageId;
    copy.textContent = message;
    actions.className = "jotes-confirmation-actions";
    cancelButton.className = "btn btn-warning";
    cancelButton.type = "button";
    cancelButton.textContent = cancelLabel;
    confirmButton.className = "btn " + (kind === "danger" ? "btn-back" : "btn-success");
    confirmButton.type = "button";
    confirmButton.textContent = confirmLabel;

    actions.appendChild(cancelButton);
    actions.appendChild(confirmButton);
    dialog.appendChild(heading);
    dialog.appendChild(copy);
    dialog.appendChild(actions);
    overlay.appendChild(dialog);

    overlay.addEventListener("click", function (event) {
      if (event.target === overlay) {
        closeActiveConfirmationDialog(false);
      }
    });
    cancelButton.addEventListener("click", function () {
      closeActiveConfirmationDialog(false);
    });
    confirmButton.addEventListener("click", function () {
      closeActiveConfirmationDialog(true);
    });

    activeConfirmationPreviouslyFocusedElement = document.activeElement;
    activeConfirmationOverlay = overlay;
    document.body.appendChild(overlay);

    activeConfirmationKeydownHandler = function (event) {
      var focusableButtons = [cancelButton, confirmButton];
      var currentFocusIndex = focusableButtons.indexOf(document.activeElement);

      if (event.key === "Escape") {
        event.preventDefault();
        closeActiveConfirmationDialog(false);
        return;
      }

      if (event.key !== "Tab") {
        return;
      }

      event.preventDefault();

      if (event.shiftKey) {
        focusableButtons[(currentFocusIndex <= 0 ? focusableButtons.length : currentFocusIndex) - 1].focus();
        return;
      }

      focusableButtons[(currentFocusIndex + 1) % focusableButtons.length].focus();
    };

    document.addEventListener("keydown", activeConfirmationKeydownHandler);
    cancelButton.focus();

    return new Promise(function (resolve) {
      activeConfirmationResolve = resolve;
    });
  }

  /**
   * Read one form element's confirmation-dialog configuration from its data
   * attributes so generic submit interception can show the correct copy.
   *
   * Parameters:
   *   - formElement: the form whose data-jotes-confirm-* attributes should be converted into dialog options.
   *
   * Returns:
   *   - object: a normalized confirmation options object compatible with showConfirmationDialog.
   */
  function readConfirmationDialogOptions(formElement) {
    return {
      title: formElement.getAttribute("data-jotes-confirm-title") || "Confirm action",
      message: formElement.getAttribute("data-jotes-confirm-message") || "Are you sure you want to continue?",
      confirmLabel: formElement.getAttribute("data-jotes-confirm-confirm-label") || "Confirm",
      cancelLabel: formElement.getAttribute("data-jotes-confirm-cancel-label") || "Cancel",
      kind: formElement.getAttribute("data-jotes-confirm-kind") || "success",
    };
  }

  /**
   * Intercept one flagged form submission, show the shared confirmation dialog,
   * and only resubmit the form after the user confirms.
   *
   * Parameters:
   *   - event: the submit event raised for the candidate form.
   *
   * Returns:
   *   - none: the form submission is paused until the dialog resolves, then resumed when confirmed.
   */
  function handleConfirmedFormSubmit(event) {
    var formElement = event.target;
    var submitter = event.submitter || null;

    if (!formElement || formElement.getAttribute("data-jotes-confirm-submit") !== "true") {
      return;
    }

    if (formElement.getAttribute("data-jotes-confirm-approved") === "true") {
      formElement.removeAttribute("data-jotes-confirm-approved");
      return;
    }

    event.preventDefault();

    showConfirmationDialog(readConfirmationDialogOptions(formElement)).then(function (confirmed) {
      if (!confirmed) {
        return;
      }

      formElement.setAttribute("data-jotes-confirm-approved", "true");

      if (typeof formElement.requestSubmit === "function") {
        if (submitter) {
          formElement.requestSubmit(submitter);
        } else {
          formElement.requestSubmit();
        }
        return;
      }

      formElement.submit();
    });
  }

  /**
   * Expose the shared confirmation helper globally and enable data-attribute
   * driven confirmation dialogs for form submissions across the site.
   *
   * Parameters:
   *   - none: the function installs the shared window helper and one document-level submit listener.
   *
   * Returns:
   *   - none: the confirmation dialog system becomes available to other scripts and templates.
   */
  function initializeConfirmationDialogs() {
    window.jotesConfirmAction = showConfirmationDialog;
    document.addEventListener("submit", handleConfirmedFormSubmit);
  }

  initializeToastNotifications();
  initializeConfirmationDialogs();
  initializeModalSettingsPopups();

  if (!searchInput || !searchResults) return;

  // ------------------------------------------------------------------ //
  // Search mode persistence                                              //
  // ------------------------------------------------------------------ //

  /**
   * Get the current search mode from localStorage.
   * Returns "name" (default), "auto", or "content".
   *
   * Parameters:
   *   - none: the function reads the persisted mode from the browser's localStorage.
   *
   * Returns:
   *   - string: the normalized persisted mode, or "name" when no stored mode is available.
   */
  function getSearchMode() {
    var storedMode = "name";

    try {
      storedMode = localStorage.getItem("jotes_search_mode") || "name";
    } catch (e) {
      console.warn("localStorage not available, defaulting to name search");
      return "name";
    }

    if (storedMode === "auto" || storedMode === "content") {
      return storedMode;
    }
    return "name";
  }

  /**
   * Save the search mode preference to localStorage.
   *
   * Parameters:
   *   - mode: the normalized mode to persist, expected to be "name", "auto", or "content".
   *
   * Returns:
   *   - none: the current preference is written to localStorage when available.
   */
  function setSearchMode(mode) {
    try {
      localStorage.setItem("jotes_search_mode", mode);
    } catch (e) {
      console.warn("Could not save search mode to localStorage:", e);
    }
  }

  // Restore saved search mode on page load.
  (function initSearchMode() {
    var savedMode = getSearchMode();

    if (savedMode === "content" && radioByContent) {
      radioByContent.checked = true;
      return;
    }
    if (savedMode === "auto" && radioByAuto) {
      radioByAuto.checked = true;
      return;
    }
    if (radioByName) {
      radioByName.checked = true;
    }
  })();

  /**
   * Return every currently open settings details element that should behave
   * like a modal popup instead of an inline dropdown.
   *
   * Parameters:
   *   - none: the function reads the cached modalSettingsDetails collection.
   *
   * Returns:
   *   - Array<HTMLElement>: each open details element backing a settings popup.
   */
  function getOpenModalSettingsDetails() {
    return modalSettingsDetails.filter(function (detailsElement) {
      return Boolean(detailsElement && detailsElement.open);
    });
  }

  /**
   * Synchronize the document-level state for fullscreen-style settings popups,
   * including scroll locking while any popup is open.
   *
   * Parameters:
   *   - none: the function reads the current open modal settings state.
   *
   * Returns:
   *   - none: documentElement and body classes are updated in place.
   */
  function syncModalSettingsPopupState() {
    var hasOpenModalSettings = getOpenModalSettingsDetails().length > 0;

    document.documentElement.classList.toggle("jotes-modal-settings-open", hasOpenModalSettings);
    if (document.body) {
      document.body.classList.toggle("jotes-modal-settings-open", hasOpenModalSettings);
    }
  }

  /**
   * Close one settings popup when it is currently open.
   *
   * Parameters:
   *   - detailsElement: the details node that should be collapsed.
   *
   * Returns:
   *   - none: the element is closed in place and document state is synchronized.
   */
  function closeModalSettingsDetails(detailsElement) {
    if (!detailsElement || !detailsElement.open) {
      syncModalSettingsPopupState();
      return;
    }

    detailsElement.open = false;
    syncModalSettingsPopupState();
  }

  /**
   * Close every open settings popup except the optional element that should
   * remain visible.
   *
   * Parameters:
   *   - exceptElement: optional details node that should stay open.
   *
   * Returns:
   *   - none: matching details elements are closed in place.
   */
  function closeOtherModalSettingsDetails(exceptElement) {
    modalSettingsDetails.forEach(function (detailsElement) {
      if (!detailsElement || detailsElement === exceptElement) {
        return;
      }
      detailsElement.open = false;
    });
    syncModalSettingsPopupState();
  }

  /**
   * Wire the directory/editor settings details widgets so they behave like
   * fullscreen modal popups with backdrop clicks, Back-button dismissal,
   * outside-click dismissal, and Escape dismissal.
   *
   * Parameters:
   *   - none: the function attaches listeners to the cached modalSettingsDetails collection and the document.
   *
   * Returns:
   *   - none: popup behavior becomes live for any matching settings control on the page.
   */
  function initializeModalSettingsPopups() {
    if (!modalSettingsDetails.length) {
      return;
    }

    modalSettingsDetails.forEach(function (detailsElement) {
      var backdropElement = detailsElement.querySelector(".settings-modal-backdrop");
      var backButton = detailsElement.querySelector(".settings-modal-back");

      detailsElement.addEventListener("toggle", function () {
        if (detailsElement.open) {
          closeOtherModalSettingsDetails(detailsElement);
        }
        syncModalSettingsPopupState();
      });

      if (backdropElement) {
        backdropElement.addEventListener("click", function (event) {
          event.preventDefault();
          event.stopPropagation();
          closeModalSettingsDetails(detailsElement);
        });
      }

      if (backButton) {
        backButton.addEventListener("click", function (event) {
          event.preventDefault();
          closeModalSettingsDetails(detailsElement);
        });
      }
    });

    document.addEventListener("click", function (event) {
      getOpenModalSettingsDetails().forEach(function (detailsElement) {
        if (detailsElement.contains(event.target)) {
          return;
        }
        closeModalSettingsDetails(detailsElement);
      });
    });

    document.addEventListener("keydown", function (event) {
      if (event.key !== "Escape") {
        return;
      }

      getOpenModalSettingsDetails().forEach(function (detailsElement) {
        closeModalSettingsDetails(detailsElement);
      });
    });
  }

  // ------------------------------------------------------------------ //
  // Search settings menu toggle                                        //
  // ------------------------------------------------------------------ //

  /**
   * Toggle the visibility of search settings dropdown menu.
   *
   * Parameters:
   *   - none: the function toggles the cached search settings menu element in place.
   *
   * Returns:
   *   - none: the menu's hidden class is flipped.
   */
  function toggleSearchSettingsMenu() {
    searchSettingsMenu.classList.toggle("hidden");
  }

  /**
   * Persist a newly selected search mode, close the menu, and rerun the
   * current query immediately when the search box already contains text.
   *
   * Parameters:
   *   - mode: the normalized mode string that should become active.
   *
   * Returns:
   *   - none: the selected mode is saved and the current query is redispatched when needed.
   */
  function handleSearchModeSelection(mode) {
    activeIdx = -1;
    cancelScheduledSearchDispatch();
    setSearchMode(mode);
    searchSettingsMenu.classList.add("hidden");

    if (searchInput.value.trim()) {
      dispatchSearch(searchInput.value);
      return;
    }

    hideResults();
  }

  // Show/hide settings menu on button click
  if (searchSettingsBtn) {
    searchSettingsBtn.addEventListener("click", function(e) {
      e.stopPropagation();
      toggleSearchSettingsMenu();
    });
  }

  // Update search mode when radio buttons are clicked.
  if (radioByName) {
    radioByName.addEventListener("change", function() {
      if (this.checked) {
        handleSearchModeSelection("name");
      }
    });
  }

  if (radioByAuto) {
    radioByAuto.addEventListener("change", function() {
      if (this.checked) {
        handleSearchModeSelection("auto");
      }
    });
  }

  if (radioByContent) {
    radioByContent.addEventListener("change", function() {
      if (this.checked) {
        handleSearchModeSelection("content");
      }
    });
  }

  // Close settings menu when clicking outside
  document.addEventListener("click", function(e) {
    if (!searchSettingsBtn.contains(e.target) && !searchSettingsMenu.contains(e.target)) {
      searchSettingsMenu.classList.add("hidden");
    }
  });

  function showResults() {
    searchResults.classList.remove("hidden");
  }

  function hideResults() {
    searchResults.classList.add("hidden");
  }

  /**
   * Render the current mixed search result list into the dropdown, supporting
   * both filename hits and content/snippet hits in the same response.
   *
   * Parameters:
   *   - items: array of normalized backend results shaped like {type, name, path, line?, text?}.
   *   - warning: optional non-fatal backend warning that should remain visible above any partial results.
   *
   * Returns:
   *   - none: the search results container is replaced in place.
   */
  function renderResults(items, warning) {
    searchResults.innerHTML = "";

    if (warning) {
      var warningState = document.createElement("div");
      warningState.className = "search-no-results search-warning";
      warningState.textContent = warning;
      searchResults.appendChild(warningState);
    }

    if (items.length === 0) {
      if (!warning) {
        var emptyState = document.createElement("div");
        emptyState.className = "search-no-results";
        emptyState.textContent = "No results found.";
        searchResults.appendChild(emptyState);
      }
      showResults();
      return;
    }

    items.slice(0, 40).forEach(function (item) {
      var link = document.createElement("a");
      var top = document.createElement("span");
      var name = document.createElement("span");
      var path = document.createElement("span");
      var isContentResult = item.type === "content";

      link.href = item.path;
      link.className = "search-result-item";

      top.className = "result-top";
      name.className = "result-name";
      name.textContent = item.name;
      top.appendChild(name);

      if (isContentResult && typeof item.line === "number") {
        var lineInfo = document.createElement("span");
        lineInfo.className = "result-line";
        lineInfo.textContent = "Line " + item.line;
        top.appendChild(lineInfo);
      }

      link.appendChild(top);

      if (isContentResult && item.text) {
        var preview = document.createElement("span");
        var truncatedText = item.text.length > 80 ? item.text.substring(0, 77) + "..." : item.text;

        preview.className = "result-preview";
        preview.textContent = truncatedText;
        link.appendChild(preview);
      }

      if (item.path) {
        path.className = "result-path";
        path.textContent = item.path;
        link.appendChild(path);
      }

      searchResults.appendChild(link);
    });

    showResults();
  }

  // ------------------------------------------------------------------ //
  // Search worker                                                        //
  // ------------------------------------------------------------------ //

  // Monotone counter — incremented on every query dispatch. The worker echoes
  // the id back with its response; any response whose id is less than lastId
  // belonged to an older query and is ignored.
  var lastId = 0;
  var worker = new Worker("/static/js/search-worker.js");
  var searchDispatchTimer = 0;
  var searchInputDebounceMs = 400;

  /**
   * Render one transient status message inside the search results popout.
   *
   * Parameters:
   *   - message: the short status string that should be shown to the user.
   *
   * Returns:
   *   - none: the search results container is replaced with a single status row and made visible.
   */
  function showSearchStatusMessage(message) {
    searchResults.innerHTML = "";

    var statusMessage = document.createElement("div");
    statusMessage.className = "search-no-results";
    statusMessage.textContent = message;
    searchResults.appendChild(statusMessage);
    showResults();
  }

  /**
   * Receive backend search responses from the worker and update the UI only for
   * the newest logical query state.
   *
   * Parameters:
   *   - e: the worker message event containing normalized search results, a warning, or an error.
   *
   * Returns:
   *   - none: the results popout is updated in place when the response is still current.
   */
  function handleSearchWorkerMessage(e) {
    if (typeof e.data.id !== "number") {
      return;
    }

    if (e.data.id < lastId) {
      return;
    }

    if (e.data.error) {
      showSearchStatusMessage(e.data.error);
      return;
    }

    if (e.data.warning) {
      console.warn("Jotes: search warning:", e.data.warning);
    }

    renderResults(e.data.results || [], e.data.warning || "");
  }

  worker.onmessage = handleSearchWorkerMessage;

  /**
   * Handle unexpected worker failures that occur before the worker can post a
   * structured response back to the page.
   *
   * Parameters:
   *   - err: the worker error event describing the failure.
   *
   * Returns:
   *   - none: the failure is logged and the current search popout shows a generic error message.
   */
  function handleSearchWorkerError(err) {
    console.warn("Jotes: search worker error:", err.message);
    showSearchStatusMessage("Search is temporarily unavailable.");
  }

  worker.onerror = handleSearchWorkerError;

  /**
   * Invalidate every older in-flight search response so later UI changes such
   * as new typing, clearing, or dismissing the results popover cannot be
   * overwritten by stale worker messages.
   *
   * Parameters:
   *   - none: the function advances the shared logical search generation counter.
   *
   * Returns:
   *   - number: the new generation identifier that should be used for the next request.
   */
  function invalidateSearchResponses() {
    lastId += 1;
    return lastId;
  }

  /**
   * Dispatch one search query to the worker using the currently selected search
   * mode.
   *
   * Parameters:
   *   - query: the raw text currently entered into the shared search field.
   *   - requestId: optional logical request identifier to reuse when a debounced input already invalidated older responses.
   *
   * Returns:
   *   - none: the worker begins an async backend search or the results popout is hidden when the query is blank.
   */
  function dispatchSearch(query, requestId) {
    var trimmedQuery = query.trim();
    var searchMode = getSearchMode();

    if (!trimmedQuery) {
      invalidateSearchResponses();
      hideResults();
      return;
    }

    if (typeof requestId !== "number") {
      requestId = invalidateSearchResponses();
    }

    showSearchStatusMessage("Searching...");
    worker.postMessage({ type: "search", query: trimmedQuery, id: requestId, mode: searchMode });
  }

  /**
   * Cancel any pending debounced search dispatch so superseded typing bursts or
   * abandoned inputs do not trigger stale backend requests.
   *
   * Parameters:
   *   - none: the function operates on the shared timeout identifier for the search field.
   *
   * Returns:
   *   - none: any pending timeout is cleared and the shared timer slot is reset.
   */
  function cancelScheduledSearchDispatch() {
    if (!searchDispatchTimer) {
      return;
    }

    window.clearTimeout(searchDispatchTimer);
    searchDispatchTimer = 0;
  }

  /**
   * Schedule one debounced search dispatch after the user stops typing for the
   * configured idle window, while invalidating older in-flight responses as
   * soon as the input changes.
   *
   * Parameters:
   *   - query: the raw text currently entered into the shared search field.
   *
   * Returns:
   *   - none: the current pending dispatch is replaced with a new 400ms delayed search, or cleared immediately when the query is blank.
   */
  function scheduleSearchDispatch(query) {
    var trimmedQuery = query.trim();
    var requestId = 0;

    cancelScheduledSearchDispatch();
    if (!trimmedQuery) {
      dispatchSearch(query);
      return;
    }

    requestId = invalidateSearchResponses();
    searchDispatchTimer = window.setTimeout(function () {
      searchDispatchTimer = 0;
      dispatchSearch(query, requestId);
    }, searchInputDebounceMs);
  }

  // ------------------------------------------------------------------ //
  // Keyboard navigation                                                  //
  // ------------------------------------------------------------------ //

  let activeIdx = -1;

  function setActive(idx) {
    const items = searchResults.querySelectorAll(".search-result-item");
    items.forEach(function (el, i) {
      el.classList.toggle("active", i === idx);
    });
    activeIdx = idx;
  }

  searchInput.addEventListener("keydown", function (e) {
    const items = searchResults.querySelectorAll(".search-result-item");
    const count = items.length;

    if (e.key === "ArrowDown") {
      e.preventDefault();
      setActive(Math.min(activeIdx + 1, count - 1));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setActive(Math.max(activeIdx - 1, 0));
    } else if (e.key === "Enter") {
      if (activeIdx >= 0 && items[activeIdx]) {
        items[activeIdx].click();
      }
    } else if (e.key === "Escape") {
      cancelScheduledSearchDispatch();
      invalidateSearchResponses();
      hideResults();
      searchInput.blur();
    }
  });

  // ------------------------------------------------------------------ //
  // Event wiring                                                         //
  // ------------------------------------------------------------------ //

  searchInput.addEventListener("input", function () {
    activeIdx = -1;
    scheduleSearchDispatch(searchInput.value);
  });

  searchInput.addEventListener("focus", function () {
    if (searchInput.value.trim()) {
      dispatchSearch(searchInput.value);
    }
  });

  document.addEventListener("click", function (e) {
    if (!searchInput.contains(e.target) && !searchResults.contains(e.target) && !searchSettingsBtn.contains(e.target) && !searchSettingsMenu.contains(e.target)) {
      cancelScheduledSearchDispatch();
      invalidateSearchResponses();
      hideResults();
    }
  });

  // ------------------------------------------------------------------ //
  // Global keyboard shortcut: press "/" to focus search                 //
  // ------------------------------------------------------------------ //

  /**
   * Report whether one input element accepts text-style entry and therefore
   * should keep slash-key input instead of yielding to the global search shortcut.
   *
   * Parameters:
   *   - element: the DOM element to inspect, expected to be an INPUT element when present.
   *
   * Returns:
   *   - boolean: true when the input is enabled, not read-only, and uses a text-entry style input type.
   */
  function isSlashShortcutTextEntryInput(element) {
    var inputType = element && typeof element.getAttribute === "function" ? String(element.getAttribute("type") || "text").toLowerCase() : "text";

    if (!element || element.disabled || element.readOnly) {
      return false;
    }

    return inputType !== "button" && inputType !== "checkbox" && inputType !== "color" && inputType !== "file" && inputType !== "hidden" && inputType !== "image" && inputType !== "radio" && inputType !== "range" && inputType !== "reset" && inputType !== "submit";
  }

  /**
   * Report whether one DOM node belongs to a text-editing surface that should
   * keep slash-key input instead of yielding to the global search shortcut.
   *
   * Parameters:
   *   - node: the EventTarget or Element associated with the current key event.
   *
   * Returns:
   *   - boolean: true when the node is a text-entry input, textarea, contenteditable surface, or an editable CodeMirror editor element.
   */
  function isSlashShortcutEditableTarget(node) {
    var element = node instanceof Element ? node : node && node.parentElement ? node.parentElement : null;
    var tagName = element && element.tagName ? element.tagName.toUpperCase() : "";
    var codeMirrorRoot = null;

    if (!element) {
      return false;
    }

    if (tagName === "INPUT") {
      return isSlashShortcutTextEntryInput(element);
    }

    if (tagName === "TEXTAREA") {
      return !element.disabled && !element.readOnly;
    }

    if (element.isContentEditable) {
      return true;
    }

    if (typeof element.closest !== "function") {
      return false;
    }

    codeMirrorRoot = element.closest(".cm-editor");
    return Boolean(codeMirrorRoot && codeMirrorRoot.querySelector('.cm-content[contenteditable="true"], .cm-content[contenteditable="plaintext-only"]'));
  }

  /**
   * Focus the shared search input when the slash shortcut is pressed outside
   * of text-editing surfaces that should receive the character themselves.
   *
   * Parameters:
   *   - event: the KeyboardEvent raised for the potential slash shortcut.
   *
   * Returns:
   *   - none: the search input is focused only when the shortcut is eligible on the current page and target.
   */
  function handleGlobalSearchSlashShortcut(event) {
    var activeElement = document.activeElement;

    if (!searchInput || event.key !== "/" || event.defaultPrevented || activeElement === searchInput) {
      return;
    }

    if (isSlashShortcutEditableTarget(activeElement) || isSlashShortcutEditableTarget(event.target)) {
      return;
    }

    event.preventDefault();
    searchInput.focus();
  }

  document.addEventListener("keydown", handleGlobalSearchSlashShortcut);

  // ------------------------------------------------------------------ //
  // Directory listing filters                                           //
  // ------------------------------------------------------------------ //

  var directoryAuxiliaryCheckbox = null;
  var directoryHiddenCheckbox = null;
  var directoryJotesCheckbox = null;

  /**
   * Read one persisted boolean preference for the directory listing settings menu.
   *
   * Parameters:
   *   - storageKey: localStorage key that stores the directory preference.
   *   - defaultValue: boolean fallback to use when the key is missing or storage is unavailable.
   *
   * Returns:
   *   - boolean: the stored preference when present, otherwise defaultValue.
   */
  function getStoredDirectoryBooleanPreference(storageKey, defaultValue) {
    try {
      var storedValue = localStorage.getItem(storageKey);
      if (storedValue === null) {
        return defaultValue;
      }
      return storedValue === "true";
    } catch (e) {
      console.warn("localStorage not available, using default directory filter preference");
      return defaultValue;
    }
  }

  /**
   * Persist one boolean preference for the directory listing settings menu.
   *
   * Parameters:
   *   - storageKey: localStorage key that should receive the preference value.
   *   - value: boolean preference to persist.
   *
   * Returns:
   *   - none: the preference is written to localStorage when available.
   */
  function setStoredDirectoryBooleanPreference(storageKey, value) {
    try {
      localStorage.setItem(storageKey, String(value));
    } catch (e) {
      console.warn("Could not save directory filter preference to localStorage:", e);
    }
  }

  /**
   * Read the persisted directory-listing preference for showing auxiliary files.
   *
   * Parameters:
   *   - none: the preference is read from localStorage using the fixed auxiliary-files key.
   *
   * Returns:
   *   - boolean: true when auxiliary files should remain visible in directory listings, or false when only Markdown, Org, and HTML notes should be shown.
   */
  function getDirectoryAuxiliaryVisibilityPreference() {
    return getStoredDirectoryBooleanPreference("jotes_view_auxiliary_files", true);
  }

  /**
   * Read the persisted directory-listing preference for showing hidden files.
   *
   * Parameters:
   *   - none: the preference is read from localStorage using the fixed hidden-files key.
   *
   * Returns:
   *   - boolean: true when hidden files and directories should remain visible in directory listings, or false when dot-prefixed entries should be filtered out.
   */
  function getDirectoryHiddenVisibilityPreference() {
    return getStoredDirectoryBooleanPreference("jotes_show_hidden_files", false);
  }

  /**
   * Read the persisted directory-listing preference for showing managed .jotes
   * companion folders and their descendants.
   *
   * Parameters:
   *   - none: the preference is read from localStorage using the fixed .jotes visibility key.
   *
   * Returns:
   *   - boolean: true when managed .jotes folders should remain visible in directory listings, otherwise false.
   */
  function getDirectoryJotesVisibilityPreference() {
    return getStoredDirectoryBooleanPreference("jotes_show_jotes_folders", false);
  }

  /**
   * Persist the directory-listing preference for showing auxiliary files.
   *
   * Parameters:
   *   - showAuxiliaryFiles: boolean indicating whether non-note files should remain visible in directory listings.
   *
   * Returns:
   *   - none: the preference is written to localStorage when available.
   */
  function setDirectoryAuxiliaryVisibilityPreference(showAuxiliaryFiles) {
    setStoredDirectoryBooleanPreference("jotes_view_auxiliary_files", showAuxiliaryFiles);
  }

  /**
   * Persist the directory-listing preference for showing hidden files.
   *
   * Parameters:
   *   - showHiddenFiles: boolean indicating whether dot-prefixed files and directories should remain visible in directory listings.
   *
   * Returns:
   *   - none: the preference is written to localStorage when available.
   */
  function setDirectoryHiddenVisibilityPreference(showHiddenFiles) {
    setStoredDirectoryBooleanPreference("jotes_show_hidden_files", showHiddenFiles);
  }

  /**
   * Persist the directory-listing preference for showing managed .jotes
   * companion folders and their descendants.
   *
   * Parameters:
   *   - showJotesFolders: boolean indicating whether managed .jotes entries should remain visible in directory listings.
   *
   * Returns:
   *   - none: the preference is written to localStorage when available.
   */
  function setDirectoryJotesVisibilityPreference(showJotesFolders) {
    setStoredDirectoryBooleanPreference("jotes_show_jotes_folders", showJotesFolders);
  }

  /**
   * Encode the current directory filter checkbox state into the same bit index
   * used by the server-rendered row visibility masks.
   *
   * Parameters:
   *   - showAuxiliaryFiles: boolean indicating whether auxiliary files should remain visible.
   *   - showHiddenFiles: boolean indicating whether generic hidden files should remain visible.
   *   - showJotesFolders: boolean indicating whether managed .jotes entries should remain visible.
   *
   * Returns:
   *   - number: the visibility-mask bit index that corresponds to the supplied filter combination.
   */
  function buildDirectoryVisibilityFilterIndex(showAuxiliaryFiles, showHiddenFiles, showJotesFolders) {
    return (showAuxiliaryFiles ? 1 : 0) + (showHiddenFiles ? 2 : 0) + (showJotesFolders ? 4 : 0);
  }

  /**
   * Copy the current directory filter preferences into the settings checkboxes.
   *
   * Parameters:
   *   - showAuxiliaryFiles: boolean indicating whether auxiliary files should appear in the current listing.
   *   - showHiddenFiles: boolean indicating whether hidden files and directories should appear in the current listing.
   *   - showJotesFolders: boolean indicating whether managed .jotes entries should appear in the current listing.
   *
   * Returns:
   *   - none: the checkbox state is updated in place when the controls exist on the page.
   */
  function syncDirectoryFilterCheckboxes(showAuxiliaryFiles, showHiddenFiles, showJotesFolders) {
    if (directoryAuxiliaryCheckbox) {
      directoryAuxiliaryCheckbox.checked = showAuxiliaryFiles;
    }
    if (directoryHiddenCheckbox) {
      directoryHiddenCheckbox.checked = showHiddenFiles;
    }
    if (directoryJotesCheckbox) {
      directoryJotesCheckbox.checked = showJotesFolders;
    }
  }

  /**
   * Recompute alternating background classes for the currently visible directory rows.
   *
   * Parameters:
   *   - directoryRows: NodeList of directory table rows that may be visible or hidden after directory filtering.
   *
   * Returns:
   *   - number: the count of rows that are currently visible and therefore participate in alternating row striping.
   */
  function updateDirectoryVisibleRowStriping(directoryRows) {
    var visibleRowCount = 0;
    var index = 0;

    for (index = 0; index < directoryRows.length; index++) {
      var row = directoryRows[index];
      if (row.hidden) {
        row.classList.remove("row-visible-odd", "row-visible-even");
        continue;
      }

      visibleRowCount++;
      row.classList.toggle("row-visible-odd", visibleRowCount % 2 === 1);
      row.classList.toggle("row-visible-even", visibleRowCount % 2 === 0);
    }

    return visibleRowCount;
  }

  /**
   * Decide whether one directory row should remain visible for the current filter combination.
   *
   * Parameters:
   *   - row: the directory-table row whose visibility metadata should be evaluated.
   *   - showAuxiliaryFiles: boolean indicating whether auxiliary files should remain visible.
   *   - showHiddenFiles: boolean indicating whether generic hidden files and directories should remain visible.
   *   - showJotesFolders: boolean indicating whether managed .jotes entries should remain visible.
   *
   * Returns:
   *   - boolean: true when the row should stay visible for the supplied filter combination, otherwise false.
   */
  function rowStaysVisibleForDirectoryFilters(row, showAuxiliaryFiles, showHiddenFiles, showJotesFolders) {
    var visibilityMask = Number.parseInt(row.getAttribute("data-entry-visibility-mask") || "0", 10);
    var filterIndex = buildDirectoryVisibilityFilterIndex(showAuxiliaryFiles, showHiddenFiles, showJotesFolders);

    if (!Number.isFinite(visibilityMask) || visibilityMask < 0) {
      return true;
    }

    return (visibilityMask & (1 << filterIndex)) !== 0;
  }

  /**
   * Apply the current directory filter preferences to the visible rows in the table.
   *
   * Parameters:
   *   - showAuxiliaryFiles: boolean indicating whether rows marked as auxiliary-only should remain visible.
   *   - showHiddenFiles: boolean indicating whether rows marked as hidden-only should remain visible.
   *   - showJotesFolders: boolean indicating whether managed .jotes entries should remain visible.
   *
   * Returns:
   *   - none: table rows are hidden or revealed in place, the visible rows are re-striped, and the filtered empty-state message is toggled when every row becomes hidden.
   */
  function applyDirectoryVisibilityPreferences(showAuxiliaryFiles, showHiddenFiles, showJotesFolders) {
    var fileTable = document.getElementById("directory-table");
    if (!fileTable) return;

    var tableBody = fileTable.tBodies.length > 0 ? fileTable.tBodies[0] : null;
    if (!tableBody) return;

    var directoryRows = tableBody.querySelectorAll("tr[data-entry-visibility-mask]");
    var filteredEmptyState = document.getElementById("dir-filter-empty");
    var visibleRowCount = 0;
    var index = 0;

    for (index = 0; index < directoryRows.length; index++) {
      var row = directoryRows[index];
      row.hidden = !rowStaysVisibleForDirectoryFilters(row, showAuxiliaryFiles, showHiddenFiles, showJotesFolders);
    }

    visibleRowCount = updateDirectoryVisibleRowStriping(directoryRows);
    fileTable.hidden = visibleRowCount === 0;
    if (filteredEmptyState) {
      filteredEmptyState.hidden = visibleRowCount !== 0;
    }
  }

  /**
   * Respond to a change in any directory settings checkbox by persisting and applying the new preferences.
   *
   * Parameters:
   *   - none: the function reads the current checkbox states from the cached directory settings controls.
   *
   * Returns:
   *   - none: the preferences are saved and the current directory listing is updated immediately.
   */
  function handleDirectoryFilterSettingsChange() {
    if (!directoryAuxiliaryCheckbox || !directoryHiddenCheckbox || !directoryJotesCheckbox) return;

    var showAuxiliaryFiles = directoryAuxiliaryCheckbox.checked;
    var showHiddenFiles = directoryHiddenCheckbox.checked;
    var showJotesFolders = directoryJotesCheckbox.checked;
    setDirectoryAuxiliaryVisibilityPreference(showAuxiliaryFiles);
    setDirectoryHiddenVisibilityPreference(showHiddenFiles);
    setDirectoryJotesVisibilityPreference(showJotesFolders);
    applyDirectoryVisibilityPreferences(showAuxiliaryFiles, showHiddenFiles, showJotesFolders);
  }

  /**
   * Initialize the directory filter UI when the current page renders a directory listing.
   *
   * Parameters:
   *   - none: the function locates the optional directory settings controls in the current document.
   *
   * Returns:
   *   - none: the checkboxes are synced from persisted state, the current table is filtered, and change handling is wired when the controls exist.
   */
  function initDirectoryFilters() {
    directoryAuxiliaryCheckbox = document.getElementById("dir-setting-view-auxiliary");
    directoryHiddenCheckbox = document.getElementById("dir-setting-show-hidden");
    directoryJotesCheckbox = document.getElementById("dir-setting-show-jotes");
    if (!directoryAuxiliaryCheckbox || !directoryHiddenCheckbox || !directoryJotesCheckbox) return;

    var showAuxiliaryFiles = getDirectoryAuxiliaryVisibilityPreference();
    var showHiddenFiles = getDirectoryHiddenVisibilityPreference();
    var showJotesFolders = getDirectoryJotesVisibilityPreference();
    syncDirectoryFilterCheckboxes(showAuxiliaryFiles, showHiddenFiles, showJotesFolders);
    applyDirectoryVisibilityPreferences(showAuxiliaryFiles, showHiddenFiles, showJotesFolders);
    directoryAuxiliaryCheckbox.addEventListener("change", handleDirectoryFilterSettingsChange);
    directoryHiddenCheckbox.addEventListener("change", handleDirectoryFilterSettingsChange);
    directoryJotesCheckbox.addEventListener("change", handleDirectoryFilterSettingsChange);
  }

  initDirectoryFilters();

  // ------------------------------------------------------------------ //
  // Row click handling (all screen sizes)                               //
  // ------------------------------------------------------------------ //

  (function () {
    var fileTable = document.querySelector(".file-table");
    if (!fileTable) return;

    fileTable.addEventListener("click", function (e) {
      var row = e.target.closest("tr");
      if (!row) return;

      var entryLink = row.querySelector(".entry-link");
      if (!entryLink) return;

      if (e.target.closest(".btn")) return;

      window.location.href = entryLink.href;
    });
  })();

  // ------------------------------------------------------------------ //
  // Copy button functionality                                           //
  // ------------------------------------------------------------------ //

  (function () {
    /**
     * Show a temporary "Copied!" state on the button.
     */
    function showCopiedState(button, duration) {
      if (!duration) duration = 1500;
      var originalText = button.textContent;
      button.textContent = "Copied!";
      button.classList.add("copied");

      setTimeout(function () {
        button.textContent = originalText;
        button.classList.remove("copied");
      }, duration);
    }

    /**
     * Copy text to clipboard using the modern Clipboard API with fallback.
     */
    function copyToClipboard(text) {
      if (navigator.clipboard && navigator.clipboard.writeText) {
        return navigator.clipboard.writeText(text).then(function () {
          return true;
        }).catch(function (err) {
          console.warn("Clipboard API failed, falling back:", err);
          return fallbackCopy(text);
        });
      } else {
        return fallbackCopy(text);
      }
    }

    /**
     * Fallback copy method using execCommand (deprecated but widely supported).
     */
    function fallbackCopy(text) {
      var textarea = document.createElement("textarea");
      textarea.value = text;
      textarea.style.position = "fixed";
      textarea.style.left = "-9999px";
      textarea.style.top = "0";
      document.body.appendChild(textarea);
      textarea.select();

      try {
        var successful = document.execCommand("copy");
        document.body.removeChild(textarea);
        return Promise.resolve(successful);
      } catch (err) {
        document.body.removeChild(textarea);
        console.error("Fallback copy failed:", err);
        return Promise.reject(err);
      }
    }

    /**
     * Extract plain text from a Chroma syntax-highlighted table.
     * The table has two columns: line numbers and code content.
     */
    function extractTextFromChroma(chromaEl) {
      var rows = chromaEl.querySelectorAll("tr");
      var lines = [];

      rows.forEach(function (row) {
        var cells = row.querySelectorAll("td");
        if (cells.length > 1) {
          // Second column contains the actual code content
          var codeCell = cells[1];
          var text = codeCell.textContent;
          lines.push(text);
        } else if (cells.length === 1) {
          // Fallback: use the only cell's text
          lines.push(cells[0].textContent);
        }
      });

      return lines.join("\n");
    }

    /**
     * Set up copy button for full text preview.
     */
    (function () {
      var textPreview = document.querySelector(".text-preview");
      if (!textPreview) return;

      var chromaEl = textPreview.querySelector(".chroma");
      if (!chromaEl) return;

      // Create the copy button
      var copyBtn = document.createElement("button");
      copyBtn.className = "copy-btn";
      copyBtn.textContent = "Copy";
      copyBtn.type = "button";
      textPreview.appendChild(copyBtn);

      // Handle click
      copyBtn.addEventListener("click", function () {
        var textContent = extractTextFromChroma(chromaEl);
        copyToClipboard(textContent).then(function () {
          showCopiedState(copyBtn);
        }).catch(function (err) {
          console.error("Failed to copy:", err);
          copyBtn.textContent = "Error";
          setTimeout(function () {
            copyBtn.textContent = "Copy";
          }, 1500);
        });
      });
    })();

    /**
     * Read the numeric level from one rendered Markdown heading element when it
     * is a direct h1-h6 node inside a rendered preview.
     *
     * Parameters:
     *   - element: the DOM element to inspect for heading semantics.
     *
     * Returns:
     *   - number: the heading level from 1 through 6 when element is an h1-h6 tag, otherwise 0.
     */
    function getRenderedMarkdownHeadingLevel(element) {
      var tagName = element && element.tagName ? element.tagName.toUpperCase() : "";

      if (!/^H[1-6]$/.test(tagName)) {
        return 0;
      }

      return Number(tagName.slice(1));
    }

    /**
     * Wrap direct Markdown body siblings that follow one heading until the next
     * heading so shared CSS can indent the full section block together with its
     * heading on previews that were rendered without server-side body wrappers.
     *
     * Parameters:
     *   - renderedPreview: the .rendered-preview root whose direct children may need Markdown section wrappers.
     *
     * Returns:
     *   - none: missing body wrappers are inserted in place, while Org previews and already-wrapped Markdown previews are left unchanged.
     */
    function wrapRenderedMarkdownHeadingBodies(renderedPreview) {
      var children = [];
      var index = 0;

      if (!renderedPreview || !renderedPreview.classList || !renderedPreview.classList.contains("rendered-preview--markdown-org")) {
        return;
      }

      if (renderedPreview.querySelector(":scope > .rendered-heading-body") || renderedPreview.querySelector(':scope > div[id^="outline-container-"]')) {
        return;
      }

      children = Array.from(renderedPreview.children);
      while (index < children.length) {
        var child = children[index];
        var headingLevel = getRenderedMarkdownHeadingLevel(child);
        var bodyNodes = [];
        var wrapper = null;

        if (!headingLevel) {
          index += 1;
          continue;
        }

        index += 1;
        while (index < children.length && !getRenderedMarkdownHeadingLevel(children[index])) {
          bodyNodes.push(children[index]);
          index += 1;
        }

        if (!bodyNodes.length) {
          continue;
        }

        wrapper = document.createElement("div");
        wrapper.className = "rendered-heading-body rendered-heading-body--level-" + String(headingLevel);
        renderedPreview.insertBefore(wrapper, bodyNodes[0]);
        bodyNodes.forEach(function (node) {
          wrapper.appendChild(node);
        });
      }
    }

    /**
     * Apply the shared post-render enhancements that give rendered Markdown and
     * Org previews their final document styling, including code-block controls,
     * heading-body wrappers for flat Markdown fragments, and rounded table shells.
     *
     * Parameters:
     *   - root: the Document or Element that may contain one or more .rendered-preview containers.
     *
     * Returns:
     *   - none: supported rendered elements are enhanced in place and previously enhanced nodes are skipped.
     */
    function enhanceRenderedPreviewContent(root) {
      var renderedPreviews = [];
      var previewIndex = 0;

      if (!root || !root.querySelectorAll) {
        return;
      }

      if (root.nodeType === 1 && root.classList && root.classList.contains("rendered-preview")) {
        renderedPreviews = [root];
      } else {
        renderedPreviews = root.querySelectorAll(".rendered-preview");
      }

      for (previewIndex = 0; previewIndex < renderedPreviews.length; previewIndex++) {
        var renderedPreview = renderedPreviews[previewIndex];
        var preElements = null;
        var tableElements = null;

        wrapRenderedMarkdownHeadingBodies(renderedPreview);
        preElements = renderedPreview.querySelectorAll("pre:not(.has-copy-btn)");
        tableElements = renderedPreview.querySelectorAll("table:not([data-rendered-table-shell])");

        preElements.forEach(function (pre) {
          var chromaEl = pre.querySelector(".chroma");
          var codeEl = pre.querySelector("code");
          var codeContent = null;
          var copyCodeBtn = null;

          if (!codeEl && !chromaEl) {
            return;
          }

          pre.classList.add("has-copy-btn");
          codeContent = document.createElement("div");
          codeContent.className = "code-content";

          while (pre.firstChild) {
            codeContent.appendChild(pre.firstChild);
          }

          pre.appendChild(codeContent);

          copyCodeBtn = document.createElement("button");
          copyCodeBtn.className = "copy-code-btn";
          copyCodeBtn.textContent = "Copy";
          copyCodeBtn.type = "button";
          pre.appendChild(copyCodeBtn);

          /**
           * Copy one rendered code block using the most accurate text source
           * available for the current block markup.
           *
           * Parameters:
           *   - none: the function closes over the current pre/code/chroma nodes for the block being enhanced.
           *
           * Returns:
           *   - none: clipboard contents are updated asynchronously and the button state reflects success or failure.
           */
          function handleCopy() {
            var textToCopy;

            if (chromaEl) {
              textToCopy = extractTextFromChroma(chromaEl);
            } else if (codeEl) {
              textToCopy = codeEl.textContent;
            } else {
              textToCopy = pre.textContent.replace(/\s*$/, "");
            }

            copyToClipboard(textToCopy).then(function () {
              showCopiedState(copyCodeBtn);
            }).catch(function (err) {
              console.error("Failed to copy:", err);
              copyCodeBtn.textContent = "Error";
              setTimeout(function () {
                copyCodeBtn.textContent = "Copy";
              }, 1500);
            });
          }

          copyCodeBtn.addEventListener("click", function (e) {
            e.stopPropagation();
            handleCopy();
          });
        });

        tableElements.forEach(function (table) {
          var shell = document.createElement("div");

          shell.className = "rendered-table-shell";
          table.dataset.renderedTableShell = "true";
          table.parentNode.insertBefore(shell, table);
          shell.appendChild(table);
        });
      }
    }

    window.jotesEnhanceRenderedPreviewContent = enhanceRenderedPreviewContent;
    window.jotesEnhanceRenderedCodeBlocks = enhanceRenderedPreviewContent;
    enhanceRenderedPreviewContent(document);

  })();

})();

// ------------------------------------------------------------------ //
// Image Lightbox with Zoom                                           //
// ------------------------------------------------------------------ //

(function () {
  "use strict";

  var overlay = null;
  var container = null;
  var wrapper = null;
  var image = null;
  var closeBtn = null;
  var zoomInBtn = null;
  var zoomOutBtn = null;
  var zoomLevelDisplay = null;
  
  var currentZoom = 1;
  var minZoom = 1;
  var maxZoom = 8;
  var zoomStep = 0.25;
  var isPanning = false;
  var panStartX = 0;
  var panStartY = 0;
  var panOffsetX = 0;
  var panOffsetY = 0;

  // Create lightbox DOM elements
  function createLightbox() {
    overlay = document.createElement('div');
    overlay.className = 'image-lightbox-overlay';
    
    container = document.createElement('div');
    container.className = 'image-lightbox-container';
    
    wrapper = document.createElement('div');
    wrapper.className = 'image-lightbox-wrapper';
    
    image = document.createElement('img');
    image.className = 'image-lightbox-image';
    image.draggable = false;
    
    closeBtn = document.createElement('button');
    closeBtn.className = 'image-lightbox-close';
    closeBtn.innerHTML = '<svg viewBox="0 0 24 24"><path d="M19 6.41L17.59 5 12 10.59 6.41 5 5 6.41 10.59 12 5 17.59 6.41 19 12 13.41 17.59 19 19 17.59 13.41 12z"/></svg>';
    closeBtn.setAttribute('aria-label', 'Close');
    
    var controls = document.createElement('div');
    controls.className = 'image-lightbox-controls';
    
    zoomOutBtn = document.createElement('button');
    zoomOutBtn.className = 'image-lightbox-zoom-btn';
    zoomOutBtn.innerHTML = '<svg viewBox="0 0 24 24"><path d="M15.5 14h-7v-1.5h7V14zm3.5-5h-11c-.83 0-1.5-.67-1.5-1.5S6.17 6 7 6h11c.83 0 1.5.67 1.5 1.5s-.67 1.5-1.5 1.5z"/></svg>';
    zoomOutBtn.setAttribute('aria-label', 'Zoom out');
    
    zoomInBtn = document.createElement('button');
    zoomInBtn.className = 'image-lightbox-zoom-btn';
    zoomInBtn.innerHTML = '<svg viewBox="0 0 24 24"><path d="M19 13h-6v6h-2v-6H5v-2h6V5h2v6h6v2z"/></svg>';
    zoomInBtn.setAttribute('aria-label', 'Zoom in');
    
    zoomLevelDisplay = document.createElement('span');
    zoomLevelDisplay.className = 'image-lightbox-zoom-level';
    zoomLevelDisplay.textContent = '100%';
    
    controls.appendChild(zoomOutBtn);
    controls.appendChild(zoomInBtn);
    controls.appendChild(zoomLevelDisplay);
    
    wrapper.appendChild(image);
    container.appendChild(wrapper);
    container.appendChild(closeBtn);
    container.appendChild(controls);
    overlay.appendChild(container);
    document.body.appendChild(overlay);

    // Event listeners
    closeBtn.addEventListener('click', closeLightbox);
    zoomInBtn.addEventListener('click', zoomIn);
    zoomOutBtn.addEventListener('click', zoomOut);
    
    // Click on image to toggle zoom
    image.addEventListener('click', function(e) {
      e.stopPropagation();
      // If mouse moved significantly, it was a drag, not a click - don't zoom
      if (mouseMoved) {
        mouseMoved = false;
        return;
      }
      if (currentZoom > minZoom && currentZoom <= 2) {
        resetZoom();
      } else {
        setZoom(currentZoom > 2 ? 2 : currentZoom + zoomStep);
      }
    });
    
    // Click on overlay closes lightbox
    overlay.addEventListener('click', function(e) {
      if (e.target === overlay || e.target === container) {
        resetZoom();
        closeLightbox();
      }
    });
    
    // Mouse wheel zoom
    container.addEventListener('wheel', handleWheel, { passive: false });
    
    // Pan functionality (mouse)
    overlay.addEventListener('mousedown', startPan);
    document.addEventListener('mousemove', pan);
    document.addEventListener('mouseup', endPan);
    
    // Touch events for mobile
    overlay.addEventListener('touchstart', handleTouchStart, { passive: true });
    overlay.addEventListener('touchmove', handleTouchMove, { passive: false });
    overlay.addEventListener('touchend', handleTouchEnd);
  }

  function openLightbox(imgSrc, altText) {
    if (!overlay) createLightbox();
    
    image.src = imgSrc;
    image.alt = altText || '';
    
    resetZoom();
    overlay.classList.add('active');
    document.body.style.overflow = 'hidden';
  }

  function closeLightbox() {
    if (!overlay) return;
    overlay.classList.remove('active');
    overlay.classList.remove('zoomed');
    image.classList.remove('zoomed');
    document.body.style.overflow = '';
  }

  function updateZoomDisplay() {
    var percentage = Math.round(currentZoom * 100);
    zoomLevelDisplay.textContent = percentage + '%';
  }

  function setZoom(zoom) {
    currentZoom = Math.max(minZoom, Math.min(maxZoom, zoom));
    // Preserve current pan offset when changing zoom
    wrapper.style.transform = 'translate(' + panOffsetX + 'px, ' + panOffsetY + 'px) scale(' + currentZoom + ')';
    updateZoomDisplay();
    
    if (currentZoom > minZoom) {
      overlay.classList.add('zoomed');
      image.classList.add('zoomed');
    } else {
      overlay.classList.remove('zoomed');
      image.classList.remove('zoomed');
    }
  }

  function zoomIn(e) {
    if (e) e.preventDefault();
    setZoom(currentZoom + zoomStep);
  }

  function zoomOut(e) {
    if (e) e.preventDefault();
    setZoom(currentZoom - zoomStep);
  }

  function resetZoom() {
    currentZoom = minZoom;
    panOffsetX = 0;
    panOffsetY = 0;
    touchPanningInitialized = false; // Reset for next pan gesture
    wrapper.style.transform = 'translate(0px, 0px) scale(' + minZoom + ')';
    updateZoomDisplay();
    overlay.classList.remove('zoomed');
    image.classList.remove('zoomed');
  }

  function handleWheel(e) {
    e.preventDefault();
    var delta = e.deltaY > 0 ? -1 : 1;
    setZoom(currentZoom + delta * zoomStep);
  }

  // Track mouse movement to distinguish click from drag
  var mouseMoved = false;
  var initialMouseX = 0;
  var initialMouseY = 0;

  // Mouse pan handlers
  function startPan(e) {
    if (currentZoom <= minZoom || e.button !== 0) return;
    isPanning = true;
    mouseMoved = false;
    panStartX = e.clientX - panOffsetX;
    panStartY = e.clientY - panOffsetY;
    initialMouseX = e.clientX;
    initialMouseY = e.clientY;
    overlay.classList.add('panning');
  }

  function pan(e) {
    if (!isPanning) return;
    // Check if mouse moved more than 3 pixels (to distinguish click from drag)
    var dx = Math.abs(e.clientX - initialMouseX);
    var dy = Math.abs(e.clientY - initialMouseY);
    if (dx > 3 || dy > 3) {
      mouseMoved = true;
    }
    e.preventDefault();
    panOffsetX = e.clientX - panStartX;
    panOffsetY = e.clientY - panStartY;
    wrapper.style.transform = 'translate(' + panOffsetX + 'px, ' + panOffsetY + 'px) scale(' + currentZoom + ')';
  }

  function endPan() {
    isPanning = false;
    if (overlay) overlay.classList.remove('panning');
  }

  // Touch handlers for mobile pinch and pan
  var touchStartDist = 0;
  var touchStartZoom = 1;
  var touchStartX = 0;
  var touchStartY = 0;
  var lastPanOffsetX = 0;
  var lastPanOffsetY = 0;
  var touchPanningInitialized = false;

  function handleTouchStart(e) {
    if (e.touches.length === 2) {
      // Pinch to zoom
      touchStartDist = getTouchDistance(e.touches);
      touchStartZoom = currentZoom;
      touchPanningInitialized = false; // Reset for next single-finger pan after pinch
    } else if (e.touches.length === 1 && currentZoom > minZoom) {
      // Single finger pan
      var touch = e.touches[0];
      touchStartX = touch.clientX - panOffsetX;
      touchStartY = touch.clientY - panOffsetY;
      lastPanOffsetX = panOffsetX;
      lastPanOffsetY = panOffsetY;
      touchPanningInitialized = true; // Already initialized on first movement
    }
  }

  function handleTouchMove(e) {
    if (e.touches.length === 2) {
      // Pinch zoom
      e.preventDefault();
      var currentDist = getTouchDistance(e.touches);
      var scale = currentDist / touchStartDist;
      setZoom(touchStartZoom * scale);
    } else if (e.touches.length === 1 && currentZoom > minZoom) {
      // Single finger pan
      e.preventDefault();
      var touch = e.touches[0];
      
      // If this is the first movement after switching from pinch, initialize pan position
      if (!touchPanningInitialized) {
        touchStartX = touch.clientX - panOffsetX;
        touchStartY = touch.clientY - panOffsetY;
        touchPanningInitialized = true;
      }
      
      panOffsetX = touch.clientX - touchStartX;
      panOffsetY = touch.clientY - touchStartY;
      wrapper.style.transform = 'translate(' + panOffsetX + 'px, ' + panOffsetY + 'px) scale(' + currentZoom + ')';
    }
  }

  function handleTouchEnd() {
    touchStartDist = 0;
    touchPanningInitialized = false; // Reset for next pan gesture
  }

  function getTouchDistance(touches) {
    var dx = touches[0].pageX - touches[1].pageX;
    var dy = touches[0].pageY - touches[1].pageY;
    return Math.sqrt(dx * dx + dy * dy);
  }

  // Keyboard controls
  document.addEventListener('keydown', function(e) {
    if (!overlay || !overlay.classList.contains('active')) return;
    
    switch (e.key) {
      case 'Escape':
        resetZoom();
        closeLightbox();
        break;
      case '+':
      case '=':
        zoomIn(e);
        break;
      case '-':
        zoomOut(e);
        break;
      case '0':
        e.preventDefault();
        resetZoom();
        break;
    }
  });

  // Make all images clickable for lightbox
  function makeImagesClickable() {
    var images = document.querySelectorAll('.image-preview img, .rendered-preview img');
    images.forEach(function(img) {
      if (!img.classList.contains('image-clickable')) {
        img.classList.add('image-clickable');
        img.addEventListener('click', function(e) {
          e.preventDefault();
          var src = this.getAttribute('src') || this.getAttribute('data-src');
          var alt = this.getAttribute('alt') || '';
          if (src) openLightbox(src, alt);
        });
      }
    });
  }

  // Initialize on page load
  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', makeImagesClickable);
  } else {
    makeImagesClickable();
  }
})();
