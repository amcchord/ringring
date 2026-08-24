#!/usr/bin/env node

import crypto from "node:crypto";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import process from "node:process";
import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";

const command = process.argv[2] ?? "inspect";
if (!new Set(["inspect", "validate", "sync", "export", "upload", "distribute"]).has(command)) {
  console.error("Usage: node scripts/appstore-connect.mjs [inspect|validate|sync|export <archive> <output> <options-plist>|upload <ipa>|distribute <build>]");
  process.exit(2);
}

const scriptDirectory = path.dirname(fileURLToPath(import.meta.url));
const rootDirectory = path.resolve(scriptDirectory, "..");
const metadataDirectory = path.join(rootDirectory, "ios", "fastlane", "metadata", "en-US");
const screenshotDirectory = path.join(rootDirectory, "ios", "fastlane", "screenshots", "en-US");
const iconPath = path.join(rootDirectory, "ios", "RingRing", "Assets.xcassets", "AppIcon.appiconset", "AppIcon-1024.png");
const bundleID = "com.mcchord.ringring";
const versionString = "1.0";
const locale = "en-US";
const screenshotDisplayType = "APP_IPHONE_67";
const apiBase = "https://api.appstoreconnect.apple.com";

function base64URL(value) {
  return Buffer.from(value).toString("base64url");
}

async function appStoreCredentials() {
  const response = await fetch("http://127.0.0.1:8472/api/keys/provision", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ service: "app-store-connect", project: "RingRing" }),
  });
  if (!response.ok) {
    throw new Error(`AustinLand credential request failed with HTTP ${response.status}`);
  }
  const payload = await response.json();
  const secrets = payload?.entry?.secrets ?? {};
  const keyID = secrets.APP_STORE_CONNECT_KEY_ID;
  const issuerID = secrets.APP_STORE_CONNECT_ISSUER_ID;
  const privateKey = secrets.APP_STORE_CONNECT_PRIVATE_KEY;
  if (!keyID || !issuerID || !privateKey?.includes("BEGIN PRIVATE KEY")) {
    throw new Error("AustinLand returned an incomplete App Store Connect credential");
  }
  return { keyID, issuerID, privateKey };
}

function makeToken({ keyID, issuerID, privateKey }) {
  const now = Math.floor(Date.now() / 1000);
  const header = base64URL(JSON.stringify({ alg: "ES256", kid: keyID, typ: "JWT" }));
  const payload = base64URL(JSON.stringify({
    iss: issuerID,
    iat: now,
    exp: now + 15 * 60,
    aud: "appstoreconnect-v1",
  }));
  const signingInput = `${header}.${payload}`;
  const signature = crypto.sign("sha256", Buffer.from(signingInput), {
    key: crypto.createPrivateKey(privateKey),
    dsaEncoding: "ieee-p1363",
  });
  return `${signingInput}.${signature.toString("base64url")}`;
}

function appleErrors(payload) {
  const errors = payload?.errors;
  if (!Array.isArray(errors)) return "unknown App Store Connect error";
  return errors.map((error) => {
    const parts = [error.status, error.code, error.title, error.detail].filter(Boolean);
    return parts.join(" · ");
  }).join("; ");
}

async function api(token, requestPath, { method = "GET", body } = {}) {
  const response = await fetch(new URL(requestPath, apiBase), {
    method,
    headers: {
      Authorization: `Bearer ${token}`,
      ...(body ? { "Content-Type": "application/json" } : {}),
    },
    body: body ? JSON.stringify(body) : undefined,
  });
  if (response.status === 204) return null;
  const payload = await response.json().catch(() => null);
  if (!response.ok) {
    throw new Error(`${method} ${requestPath} failed: ${appleErrors(payload)}`);
  }
  return payload;
}

function metadataFile(name) {
  return fs.readFileSync(path.join(metadataDirectory, `${name}.txt`), "utf8").trim();
}

function desiredMetadata() {
  const metadata = {
    name: metadataFile("name"),
    subtitle: metadataFile("subtitle"),
    privacyPolicyUrl: metadataFile("privacy_url"),
    description: metadataFile("description"),
    keywords: metadataFile("keywords"),
    marketingUrl: metadataFile("marketing_url"),
    promotionalText: metadataFile("promotional_text"),
    supportUrl: metadataFile("support_url"),
  };
  const limits = [
    ["name", 30, false],
    ["subtitle", 30, false],
    ["promotionalText", 170, false],
    ["description", 4000, false],
    ["keywords", 100, true],
  ];
  for (const [name, limit, bytes] of limits) {
    const length = bytes ? Buffer.byteLength(metadata[name]) : [...metadata[name]].length;
    if (length > limit) throw new Error(`${name} is ${length}; App Store limit is ${limit}`);
  }
  for (const name of ["privacyPolicyUrl", "marketingUrl", "supportUrl"]) {
    const url = new URL(metadata[name]);
    if (url.protocol !== "https:") throw new Error(`${name} must use HTTPS`);
  }
  return metadata;
}

function imageProperties(file) {
  const output = execFileSync("sips", ["-g", "pixelWidth", "-g", "pixelHeight", "-g", "hasAlpha", file], { encoding: "utf8" });
  const width = Number(output.match(/pixelWidth: (\d+)/)?.[1]);
  const height = Number(output.match(/pixelHeight: (\d+)/)?.[1]);
  const hasAlpha = output.match(/hasAlpha: (\w+)/)?.[1];
  return { width, height, hasAlpha };
}

function screenshotFiles() {
  if (!fs.existsSync(screenshotDirectory)) return [];
  const files = fs.readdirSync(screenshotDirectory)
    .filter((name) => /\.(png|jpe?g)$/i.test(name))
    .sort()
    .map((name) => path.join(screenshotDirectory, name));
  if (files.length < 1 || files.length > 10) {
    throw new Error(`Expected 1–10 screenshots, found ${files.length}`);
  }
  for (const file of files) {
    const properties = imageProperties(file);
    if (properties.width !== 1320 || properties.height !== 2868) {
      throw new Error(`${path.basename(file)} is ${properties.width}x${properties.height}; expected 1320x2868`);
    }
    if (properties.hasAlpha === "yes") {
      throw new Error(`${path.basename(file)} contains an alpha channel, which App Store Connect rejects`);
    }
  }
  return files;
}

async function findApp(token) {
  const payload = await api(token, `/v1/apps?filter%5BbundleId%5D=${encodeURIComponent(bundleID)}&limit=2`);
  if (payload.data.length !== 1) throw new Error(`Expected one app for ${bundleID}, found ${payload.data.length}`);
  return payload.data[0];
}

async function versions(token, appID) {
  const payload = await api(token, `/v1/apps/${appID}/appStoreVersions?filter%5Bplatform%5D=IOS&limit=50`);
  return payload.data;
}

async function appInfos(token, appID) {
  const payload = await api(token, `/v1/apps/${appID}/appInfos?limit=50`);
  return payload.data;
}

async function versionLocalizations(token, versionID) {
  const payload = await api(token, `/v1/appStoreVersions/${versionID}/appStoreVersionLocalizations?limit=50`);
  return payload.data;
}

async function appInfoLocalizations(token, appInfoID) {
  const payload = await api(token, `/v1/appInfos/${appInfoID}/appInfoLocalizations?limit=50`);
  return payload.data;
}

async function screenshotSets(token, localizationID) {
  const payload = await api(token, `/v1/appStoreVersionLocalizations/${localizationID}/appScreenshotSets?limit=50`);
  return payload.data;
}

async function screenshots(token, setID) {
  const payload = await api(token, `/v1/appScreenshotSets/${setID}/appScreenshots?limit=50`);
  return payload.data;
}

async function inspect(token, app) {
  const allVersions = await versions(token, app.id);
  const infos = await appInfos(token, app.id);
  console.log(`App: ${app.attributes.name} (${bundleID})`);
  console.log(`Icon source: ${imageProperties(iconPath).width}x${imageProperties(iconPath).height} build asset`);
  for (const info of infos) {
    const primary = await api(token, `/v1/appInfos/${info.id}/primaryCategory`);
    const secondary = await api(token, `/v1/appInfos/${info.id}/secondaryCategory`);
    console.log(`Categories: ${primary?.data?.attributes?.name ?? primary?.data?.id ?? "unset"} / ${secondary?.data?.attributes?.name ?? secondary?.data?.id ?? "unset"}`);
  }
  if (allVersions.length === 0) {
    console.log("App Store versions: none");
    return;
  }
  for (const version of allVersions) {
    console.log(`Version: ${version.attributes.versionString} · ${version.attributes.appStoreState} · copyright ${version.attributes.copyright || "unset"}`);
    const localizations = await versionLocalizations(token, version.id);
    for (const localization of localizations) {
      const sets = await screenshotSets(token, localization.id);
      const summaries = [];
      for (const set of sets) {
        const images = await screenshots(token, set.id);
        const complete = images.filter((image) => image.attributes.assetDeliveryState?.state === "COMPLETE").length;
        summaries.push(`${set.attributes.screenshotDisplayType}: ${complete}/${images.length} complete`);
      }
      console.log(`  ${localization.attributes.locale}: ${summaries.join(", ") || "no screenshots"}`);
    }
  }
}

async function ensureVersion(token, app) {
  const existing = (await versions(token, app.id)).find((version) => version.attributes.versionString === versionString);
  if (existing) return existing;
  const payload = await api(token, "/v1/appStoreVersions", {
    method: "POST",
    body: {
      data: {
        type: "appStoreVersions",
        attributes: { platform: "IOS", versionString },
        relationships: { app: { data: { type: "apps", id: app.id } } },
      },
    },
  });
  console.log(`Created App Store version ${versionString}`);
  return payload.data;
}

async function ensureVersionLocalization(token, version, metadata) {
  let localization = (await versionLocalizations(token, version.id)).find((item) => item.attributes.locale === locale);
  const attributes = {
    description: metadata.description,
    keywords: metadata.keywords,
    marketingUrl: metadata.marketingUrl,
    promotionalText: metadata.promotionalText,
    supportUrl: metadata.supportUrl,
  };
  if (!localization) {
    const payload = await api(token, "/v1/appStoreVersionLocalizations", {
      method: "POST",
      body: {
        data: {
          type: "appStoreVersionLocalizations",
          attributes: { locale, ...attributes },
          relationships: { appStoreVersion: { data: { type: "appStoreVersions", id: version.id } } },
        },
      },
    });
    console.log(`Created ${locale} version localization`);
    return payload.data;
  }
  const payload = await api(token, `/v1/appStoreVersionLocalizations/${localization.id}`, {
    method: "PATCH",
    body: { data: { type: "appStoreVersionLocalizations", id: localization.id, attributes } },
  });
  console.log(`Updated ${locale} description, keywords, URLs, and promotional text`);
  return payload.data;
}

async function ensureCategories(token, app) {
  const infos = await appInfos(token, app.id);
  if (infos.length === 0) throw new Error("App Store Connect returned no app info resource");
  const appInfo = infos.find((item) => item.attributes.appStoreState === "PREPARE_FOR_SUBMISSION") ?? infos[0];
  const payload = await api(token, "/v1/appCategories?filter%5Bplatforms%5D=IOS&limit=200");
  const utilities = payload.data.find((category) => category.id === "UTILITIES");
  const socialNetworking = payload.data.find((category) => category.id === "SOCIAL_NETWORKING");
  if (!utilities || !socialNetworking) {
    throw new Error("Could not resolve the Utilities and Social Networking App Store categories");
  }
  await api(token, `/v1/appInfos/${appInfo.id}`, {
    method: "PATCH",
    body: {
      data: {
        type: "appInfos",
        id: appInfo.id,
        relationships: {
          primaryCategory: { data: { type: "appCategories", id: utilities.id } },
          secondaryCategory: { data: { type: "appCategories", id: socialNetworking.id } },
        },
      },
    },
  });
  console.log("Set categories to Utilities / Social Networking");
}

async function ensureCopyright(token, version) {
  await api(token, `/v1/appStoreVersions/${version.id}`, {
    method: "PATCH",
    body: {
      data: {
        type: "appStoreVersions",
        id: version.id,
        attributes: { copyright: "2026 Austin McChord" },
      },
    },
  });
  console.log("Set version copyright to 2026 Austin McChord");
}

async function ensureAppInfoLocalization(token, app, metadata) {
  const infos = await appInfos(token, app.id);
  if (infos.length === 0) throw new Error("App Store Connect returned no app info resource");
  const appInfo = infos.find((item) => item.attributes.appStoreState === "PREPARE_FOR_SUBMISSION") ?? infos[0];
  let localization = (await appInfoLocalizations(token, appInfo.id)).find((item) => item.attributes.locale === locale);
  const attributes = {
    name: metadata.name,
    subtitle: metadata.subtitle,
    privacyPolicyUrl: metadata.privacyPolicyUrl,
  };
  if (!localization) {
    const payload = await api(token, "/v1/appInfoLocalizations", {
      method: "POST",
      body: {
        data: {
          type: "appInfoLocalizations",
          attributes: { locale, ...attributes },
          relationships: { appInfo: { data: { type: "appInfos", id: appInfo.id } } },
        },
      },
    });
    console.log(`Created ${locale} app information`);
    return payload.data;
  }
  const payload = await api(token, `/v1/appInfoLocalizations/${localization.id}`, {
    method: "PATCH",
    body: { data: { type: "appInfoLocalizations", id: localization.id, attributes } },
  });
  console.log(`Updated ${locale} app name, subtitle, and privacy URL`);
  return payload.data;
}

async function ensureScreenshotSet(token, localization) {
  const existing = (await screenshotSets(token, localization.id))
    .find((set) => set.attributes.screenshotDisplayType === screenshotDisplayType);
  if (existing) return existing;
  const payload = await api(token, "/v1/appScreenshotSets", {
    method: "POST",
    body: {
      data: {
        type: "appScreenshotSets",
        attributes: { screenshotDisplayType },
        relationships: {
          appStoreVersionLocalization: {
            data: { type: "appStoreVersionLocalizations", id: localization.id },
          },
        },
      },
    },
  });
  console.log(`Created ${screenshotDisplayType} screenshot set`);
  return payload.data;
}

async function uploadScreenshot(token, set, file) {
  const bytes = fs.readFileSync(file);
  const reservation = await api(token, "/v1/appScreenshots", {
    method: "POST",
    body: {
      data: {
        type: "appScreenshots",
        attributes: { fileName: path.basename(file), fileSize: bytes.length },
        relationships: { appScreenshotSet: { data: { type: "appScreenshotSets", id: set.id } } },
      },
    },
  });
  const screenshot = reservation.data;
  for (const operation of screenshot.attributes.uploadOperations) {
    const offset = Number(operation.offset);
    const length = Number(operation.length);
    const headers = Object.fromEntries((operation.requestHeaders ?? []).map((header) => [header.name, header.value]));
    const upload = await fetch(operation.url, {
      method: operation.method,
      headers,
      body: bytes.subarray(offset, offset + length),
    });
    if (!upload.ok) throw new Error(`Asset upload failed with HTTP ${upload.status}`);
  }
  const checksum = crypto.createHash("md5").update(bytes).digest("hex");
  await api(token, `/v1/appScreenshots/${screenshot.id}`, {
    method: "PATCH",
    body: {
      data: {
        type: "appScreenshots",
        id: screenshot.id,
        attributes: { uploaded: true, sourceFileChecksum: checksum },
      },
    },
  });
  for (let attempt = 0; attempt < 40; attempt += 1) {
    const current = await api(token, `/v1/appScreenshots/${screenshot.id}`);
    const state = current.data.attributes.assetDeliveryState?.state;
    if (state === "COMPLETE") {
      console.log(`Uploaded ${path.basename(file)}`);
      return current.data;
    }
    if (state === "FAILED") {
      throw new Error(`${path.basename(file)} failed App Store processing`);
    }
    await new Promise((resolve) => setTimeout(resolve, 3000));
  }
  throw new Error(`${path.basename(file)} did not finish processing within two minutes`);
}

async function replaceScreenshots(token, localization, files) {
  const set = await ensureScreenshotSet(token, localization);
  const previous = await screenshots(token, set.id);
  const alreadyCurrent = previous.length === files.length && previous.every((screenshot, index) => {
    const bytes = fs.readFileSync(files[index]);
    const checksum = crypto.createHash("md5").update(bytes).digest("hex");
    return screenshot.attributes.fileName === path.basename(files[index]) &&
      screenshot.attributes.sourceFileChecksum === checksum &&
      screenshot.attributes.assetDeliveryState?.state === "COMPLETE";
  });
  if (alreadyCurrent) {
    console.log(`Screenshot set already contains the ${files.length} current images`);
    return;
  }
  if (previous.length + files.length > 10) {
    throw new Error(`Replacing ${previous.length} screenshots with ${files.length} would exceed Apple's 10-image limit before the safe cleanup step`);
  }
  const uploaded = [];
  for (const file of files) uploaded.push(await uploadScreenshot(token, set, file));
  for (const screenshot of previous) {
    await api(token, `/v1/appScreenshots/${screenshot.id}`, { method: "DELETE" });
  }
  console.log(`Screenshot set now contains ${uploaded.length} current images`);
}

async function uploadBuild(credentials, requestedPath) {
  if (!requestedPath) throw new Error("upload requires the path to an exported IPA");
  const ipaPath = path.resolve(requestedPath);
  if (path.extname(ipaPath).toLowerCase() !== ".ipa" || !fs.statSync(ipaPath).isFile()) {
    throw new Error("upload requires an existing .ipa file");
  }
  const temporaryDirectory = fs.mkdtempSync(path.join(os.tmpdir(), "ringring-appstore-"));
  const privateKeyPath = path.join(temporaryDirectory, "AuthKey.p8");
  try {
    fs.writeFileSync(privateKeyPath, credentials.privateKey, { mode: 0o600 });
    const authentication = [
      "--api-key", credentials.keyID,
      "--api-issuer", credentials.issuerID,
      "--p8-file-path", privateKeyPath,
    ];
    try {
      execFileSync("xcrun", ["altool", "--validate-app", ipaPath, ...authentication], { stdio: "inherit" });
      execFileSync("xcrun", ["altool", "--upload-package", ipaPath, "--wait", ...authentication], { stdio: "inherit" });
    } catch {
      throw new Error("App Store validation or upload failed");
    }
  } finally {
    fs.rmSync(temporaryDirectory, { recursive: true, force: true });
  }
  console.log(`Validated and uploaded ${path.basename(ipaPath)}`);
}

async function exportBuild(credentials, requestedArchive, requestedOutput, requestedOptions) {
  if (!requestedArchive || !requestedOutput || !requestedOptions) {
    throw new Error("export requires archive, output-directory, and export-options paths");
  }
  const archivePath = path.resolve(requestedArchive);
  const outputPath = path.resolve(requestedOutput);
  const optionsPath = path.resolve(requestedOptions);
  if (path.extname(archivePath).toLowerCase() !== ".xcarchive" || !fs.statSync(archivePath).isDirectory()) {
    throw new Error("export requires an existing .xcarchive directory");
  }
  if (!fs.statSync(optionsPath).isFile()) throw new Error("export requires an existing options plist");
  fs.mkdirSync(outputPath, { recursive: true });
  const temporaryDirectory = fs.mkdtempSync(path.join(os.tmpdir(), "ringring-appstore-"));
  const privateKeyPath = path.join(temporaryDirectory, "AuthKey.p8");
  try {
    fs.writeFileSync(privateKeyPath, credentials.privateKey, { mode: 0o600 });
    try {
      execFileSync("xcodebuild", [
        "-exportArchive",
        "-archivePath", archivePath,
        "-exportPath", outputPath,
        "-exportOptionsPlist", optionsPath,
        "-allowProvisioningUpdates",
        "-authenticationKeyPath", privateKeyPath,
        "-authenticationKeyID", credentials.keyID,
        "-authenticationKeyIssuerID", credentials.issuerID,
      ], {
        stdio: "inherit",
        env: { ...process.env, PATH: `/usr/bin:/bin:/usr/sbin:/sbin:${process.env.PATH ?? ""}` },
      });
    } catch {
      throw new Error("Xcode App Store export failed");
    }
  } finally {
    fs.rmSync(temporaryDirectory, { recursive: true, force: true });
  }
  console.log(`Exported ${path.basename(archivePath)} for App Store Connect`);
}

async function distributeBuild(token, app, requestedBuild) {
  if (!/^\d+$/.test(requestedBuild ?? "")) throw new Error("distribute requires a numeric build number");
  const query = new URLSearchParams({
    "filter[app]": app.id,
    "filter[version]": requestedBuild,
    limit: "5",
  });
  const candidates = (await api(token, `/v1/builds?${query}`)).data;
  const build = candidates.find((item) => item.attributes.version === requestedBuild);
  if (!build) throw new Error(`App Store Connect does not contain build ${requestedBuild}`);
  if (build.attributes.processingState !== "VALID") {
    throw new Error(`build ${requestedBuild} is ${build.attributes.processingState}, not VALID`);
  }

  const groups = (await api(token, `/v1/apps/${app.id}/betaGroups?limit=50`)).data;
  const group = groups.find((item) => item.attributes.name === "RingRing Internal" && item.attributes.isInternalGroup === true);
  if (!group) throw new Error("the RingRing Internal TestFlight group is unavailable");

  const notes = metadataFile("release_notes");
  const localizations = (await api(token, `/v1/builds/${build.id}/betaBuildLocalizations?limit=50`)).data;
  const localization = localizations.find((item) => item.attributes.locale === locale);
  if (localization) {
    if (localization.attributes.whatsNew !== notes) {
      await api(token, `/v1/betaBuildLocalizations/${localization.id}`, {
        method: "PATCH",
        body: { data: { type: "betaBuildLocalizations", id: localization.id, attributes: { whatsNew: notes } } },
      });
    }
  } else {
    await api(token, "/v1/betaBuildLocalizations", {
      method: "POST",
      body: {
        data: {
          type: "betaBuildLocalizations",
          attributes: { locale, whatsNew: notes },
          relationships: { build: { data: { type: "builds", id: build.id } } },
        },
      },
    });
  }

  const attached = (await api(token, `/v1/betaGroups/${group.id}/builds?limit=50`)).data;
  if (!attached.some((item) => item.id === build.id)) {
    await api(token, `/v1/builds/${build.id}/relationships/betaGroups`, {
      method: "POST",
      body: { data: [{ type: "betaGroups", id: group.id }] },
    });
  }
  const verified = (await api(token, `/v1/betaGroups/${group.id}/builds?limit=50`)).data;
  if (!verified.some((item) => item.id === build.id)) {
    throw new Error(`build ${requestedBuild} was not attached to RingRing Internal`);
  }
  console.log(`Build ${requestedBuild} is VALID and available to RingRing Internal with current test notes`);
}

async function main() {
  if (command === "validate") {
    desiredMetadata();
    const files = screenshotFiles();
    const icon = imageProperties(iconPath);
    if (icon.width !== 1024 || icon.height !== 1024 || icon.hasAlpha === "yes") {
      throw new Error("The AppIcon build asset must be an opaque 1024x1024 image");
    }
    console.log(`Validated metadata, ${files.length} screenshots, and the 1024x1024 build icon`);
    return;
  }
  const credentials = await appStoreCredentials();
  if (command === "export") {
    await exportBuild(credentials, process.argv[3], process.argv[4], process.argv[5]);
    return;
  }
  if (command === "upload") {
    await uploadBuild(credentials, process.argv[3]);
    return;
  }
  const token = makeToken(credentials);
  const app = await findApp(token);
  if (command === "distribute") {
    await distributeBuild(token, app, process.argv[3]);
    return;
  }
  if (command === "inspect") {
    await inspect(token, app);
    return;
  }
  const metadata = desiredMetadata();
  const files = screenshotFiles();
  const version = await ensureVersion(token, app);
  if (version.attributes.appStoreState !== "PREPARE_FOR_SUBMISSION") {
    throw new Error(`Version ${versionString} is ${version.attributes.appStoreState}; screenshots are editable only while preparing a submission`);
  }
  await ensureCategories(token, app);
  await ensureCopyright(token, version);
  await ensureAppInfoLocalization(token, app, metadata);
  const localization = await ensureVersionLocalization(token, version, metadata);
  await replaceScreenshots(token, localization, files);
  await inspect(token, app);
}

main().catch((error) => {
  console.error(`App Store sync failed: ${error.message}`);
  process.exitCode = 1;
});
