(() => {
  "use strict";

  const buttons = document.querySelectorAll("[data-copy-target], [data-copy-setup]");
  if (buttons.length === 0) {
    return;
  }

  const status = document.getElementById("setup-copy-status");

  function legacyCopy(text) {
    const helper = document.createElement("textarea");
    helper.value = text;
    helper.setAttribute("readonly", "");
    helper.style.position = "fixed";
    helper.style.opacity = "0";
    document.body.appendChild(helper);
    helper.select();
    const copied = document.execCommand("copy");
    helper.remove();
    if (!copied) {
      throw new Error("copy unavailable");
    }
  }

  async function copyText(text) {
    if (navigator.clipboard && window.isSecureContext) {
      try {
        await navigator.clipboard.writeText(text);
        return;
      } catch (_) {
        // Some browsers expose the API but deny it. Try the user-gesture
        // fallback without retaining the copied value in the document.
      }
    }
    legacyCopy(text);
  }

  function exactValue(target) {
    if (!target) {
      return "";
    }
    if ("value" in target) {
      return target.value;
    }
    const setupValue = target.getAttribute("data-setup-value");
    return setupValue || target.textContent.trim();
  }

  function setupText() {
    const lines = ["RingRing phone setup"];
    document.querySelectorAll("[data-setup-field]").forEach((row) => {
      const label = row.querySelector("dt");
      const value = row.querySelector("[data-setup-value]");
      if (label && value) {
        const plainLabel = label.cloneNode(true);
        plainLabel.querySelectorAll(".credential-format").forEach((hint) => hint.remove());
        lines.push(`${plainLabel.textContent.trim()}: ${exactValue(value)}`);
      }
    });
    lines.push("", "Private family network only — no regular or emergency calls.");
    return lines.join("\n");
  }

  buttons.forEach((button) => {
    button.hidden = false;
    const originalLabel = button.textContent;
    const originalAriaLabel = button.getAttribute("aria-label");
    button.addEventListener("click", async () => {
      const targetID = button.getAttribute("data-copy-target");
      const target = targetID ? document.getElementById(targetID) : null;
      const text = button.hasAttribute("data-copy-setup")
        ? setupText()
        : exactValue(target);
      if (!text) {
        return;
      }
      const copyLabel = button.getAttribute("data-copy-label") || "All six settings";
      try {
        await copyText(text);
        button.textContent = "Copied!";
        button.setAttribute("aria-label", `${copyLabel} copied`);
        if (status) {
          status.textContent = `${copyLabel} copied. Keep private settings only as long as needed.`;
        }
      } catch (_) {
        button.textContent = "Select manually";
        button.setAttribute("aria-label", `${copyLabel} needs manual copying`);
        if (target && typeof target.select === "function") {
          target.focus();
          target.select();
        }
        if (status) {
          status.textContent = "Automatic copying was unavailable. Select the value and copy it manually.";
        }
      }
      window.setTimeout(() => {
        button.textContent = originalLabel;
        if (originalAriaLabel) {
          button.setAttribute("aria-label", originalAriaLabel);
        } else {
          button.removeAttribute("aria-label");
        }
      }, 2400);
    });
  });
})();
