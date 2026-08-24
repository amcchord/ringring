(() => {
  "use strict";

  let phonebook = document.getElementById("phonebook-live");
  if (!phonebook) return;

  const liveURL = phonebook.dataset.partyLiveUrl;
  if (!liveURL || !liveURL.startsWith("/parties/") || !liveURL.endsWith("/live")) return;

  const timerStates = new WeakMap();
  const formatDuration = (seconds) => {
    const hours = Math.floor(seconds / 3600);
    const minutes = Math.floor((seconds % 3600) / 60);
    const remainder = seconds % 60;
    if (hours > 0) return `${hours}:${String(minutes).padStart(2, "0")}:${String(remainder).padStart(2, "0")}`;
    return `${minutes}:${String(remainder).padStart(2, "0")}`;
  };
  const updateTimers = () => {
    const now = Date.now();
    phonebook.querySelectorAll("[data-call-seconds]").forEach((timer) => {
      const initialSeconds = Number(timer.dataset.callSeconds);
      if (!Number.isInteger(initialSeconds) || initialSeconds < 0 || initialSeconds > 2678400) return;
      let state = timerStates.get(timer);
      if (!state || state.initialSeconds !== initialSeconds) {
        state = { initialSeconds, startedAt: now };
        timerStates.set(timer, state);
      }
      const elapsedSeconds = state.initialSeconds + Math.floor((now - state.startedAt) / 1000);
      timer.textContent = formatDuration(elapsedSeconds);
      timer.dateTime = `PT${elapsedSeconds}S`;
    });
  };

  const refresh = async () => {
    if (document.hidden || !phonebook.isConnected) return;
    if (phonebook.contains(document.activeElement) || phonebook.querySelector("details[open]")) return;

    try {
      const response = await fetch(liveURL, {
        method: "GET",
        credentials: "same-origin",
        cache: "no-store",
        redirect: "error",
        headers: { Accept: "text/html" },
      });
      if (!response.ok || !response.headers.get("content-type")?.startsWith("text/html")) return;

      const page = new DOMParser().parseFromString(await response.text(), "text/html");
      const nextPhonebook = page.getElementById("phonebook-live");
      if (!nextPhonebook || nextPhonebook.dataset.partyLiveUrl !== liveURL) return;
      if (nextPhonebook.innerHTML === phonebook.innerHTML) return;

      phonebook.replaceWith(nextPhonebook);
      phonebook = document.getElementById("phonebook-live");
      updateTimers();
    } catch (_) {
      // The current phonebook remains useful through brief network or AMI outages.
    }
  };

  updateTimers();
  window.setInterval(updateTimers, 1000);
  window.setInterval(refresh, 3000);
  document.addEventListener("visibilitychange", () => {
    if (!document.hidden) refresh();
  });
})();
