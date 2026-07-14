document.addEventListener("DOMContentLoaded", () => {
  const maxSourceBytes = 20 << 20;
  const maxSourceDimension = 12000;
  const maxOutputBytes = 96 << 10;
  const outputSizes = [512, 448, 384, 320, 256];
  const outputQualities = [0.9, 0.8, 0.7, 0.6, 0.5, 0.4];
  const supportedTypes = new Set(["image/jpeg", "image/png"]);

  const cropperTemplate = `
    <cropper-canvas background scale-step="0.1">
      <cropper-image initial-center-size="cover" translatable scalable></cropper-image>
      <cropper-shade hidden></cropper-shade>
      <cropper-handle action="move" plain></cropper-handle>
      <cropper-selection
        initial-coverage="0.72"
        initial-aspect-ratio="1"
        aspect-ratio="1"
        movable
        resizable
        keyboard
        outlined
      >
        <cropper-grid role="grid" bordered covered></cropper-grid>
        <cropper-crosshair centered></cropper-crosshair>
        <cropper-handle action="move" theme-color="rgba(255, 255, 255, 0.35)"></cropper-handle>
        <cropper-handle action="n-resize"></cropper-handle>
        <cropper-handle action="e-resize"></cropper-handle>
        <cropper-handle action="s-resize"></cropper-handle>
        <cropper-handle action="w-resize"></cropper-handle>
        <cropper-handle action="ne-resize"></cropper-handle>
        <cropper-handle action="nw-resize"></cropper-handle>
        <cropper-handle action="se-resize"></cropper-handle>
        <cropper-handle action="sw-resize"></cropper-handle>
      </cropper-selection>
    </cropper-canvas>
  `;

  const form = document.querySelector(".profile-form");
  const input = document.getElementById("photo_input");
  const photoButton = document.getElementById("photo_button");
  const preview = document.getElementById("profile_photo");
  const placeholder = document.getElementById("profile_photo_placeholder");
  const photoChanged = document.getElementById("photo_changed");
  const photoBase64 = document.getElementById("photo_base64");
  const clientError = document.getElementById("photo_client_error");
  const clientStatus = document.getElementById("photo_client_status");
  const saveButton = document.getElementById("profile_save_button");

  const dialog = document.getElementById("photo_crop_dialog");
  const cropperContainer = document.getElementById("photo_cropper");
  const cropError = document.getElementById("photo_crop_error");
  const closeButton = document.getElementById("photo_crop_close");
  const cancelButton = document.getElementById("photo_crop_cancel");
  const applyButton = document.getElementById("photo_crop_apply");
  const zoomOutButton = document.getElementById("photo_zoom_out");
  const resetButton = document.getElementById("photo_crop_reset");
  const zoomInButton = document.getElementById("photo_zoom_in");

  const CropperConstructor = window.Cropper && window.Cropper.default;

  if (
    !form || !input || !photoButton || !preview || !photoChanged || !photoBase64 ||
    !dialog || !cropperContainer || !applyButton || !CropperConstructor
  ) {
    return;
  }

  let cropper = null;
  let sourceURL = "";
  let processing = false;

  function setMessage(element, message) {
    if (!element) return;
    element.textContent = message;
    element.hidden = message === "";
  }

  function setProcessing(value) {
    processing = value;
    applyButton.disabled = value;
    applyButton.textContent = value ? "画像を処理中…" : "この範囲を使用";
    if (saveButton) saveButton.disabled = value;
  }

  function releaseSource() {
    cropper = null;
    cropperContainer.replaceChildren();
    if (sourceURL) {
      URL.revokeObjectURL(sourceURL);
      sourceURL = "";
    }
  }

  function closeDialog() {
    if (processing) return;
    dialog.close();
    input.value = "";
    releaseSource();
    setMessage(cropError, "");
  }

  function validateSource(file) {
    if (!supportedTypes.has(file.type)) {
      return "JPEG または PNG ファイルを選択してください。";
    }
    if (file.size === 0) {
      return "空のファイルは使用できません。";
    }
    if (file.size > maxSourceBytes) {
      return "元画像は 20 MiB 以下のものを選択してください。";
    }
    return "";
  }

  function loadImage(file) {
    return new Promise((resolve, reject) => {
      sourceURL = URL.createObjectURL(file);
      const image = new Image();
      image.alt = "切り抜く画像";
      image.onload = () => resolve(image);
      image.onerror = () => reject(new Error("画像を読み込めませんでした。"));
      image.src = sourceURL;
    });
  }

  function canvasToJPEG(canvas, quality) {
    return new Promise((resolve, reject) => {
      canvas.toBlob((blob) => {
        if (blob) resolve(blob);
        else reject(new Error("JPEG に変換できませんでした。"));
      }, "image/jpeg", quality);
    });
  }

  function blobToDataURL(blob) {
    return new Promise((resolve, reject) => {
      const reader = new FileReader();
      reader.onload = () => resolve(reader.result);
      reader.onerror = () => reject(new Error("画像データを作成できませんでした。"));
      reader.readAsDataURL(blob);
    });
  }

  async function encodeSelection(selection) {
    for (const size of outputSizes) {
      const canvas = await selection.$toCanvas({
        width: size,
        height: size,
        beforeDraw(context) {
          context.fillStyle = "#fff";
          context.fillRect(0, 0, size, size);
        },
      });

      for (const quality of outputQualities) {
        const blob = await canvasToJPEG(canvas, quality);
        if (blob.size <= maxOutputBytes) {
          return { blob, size };
        }
      }
    }

    throw new Error("画像を 96 KiB 以下に圧縮できませんでした。切り抜く範囲を変更してください。");
  }

  photoButton.addEventListener("click", () => input.click());

  input.addEventListener("change", async () => {
    const file = input.files && input.files[0];
    if (!file) return;

    setMessage(clientError, "");
    setMessage(clientStatus, "");
    setMessage(cropError, "");

    const validationError = validateSource(file);
    if (validationError) {
      input.value = "";
      setMessage(clientError, validationError);
      return;
    }

    try {
      const image = await loadImage(file);
      if (
        image.naturalWidth < 1 || image.naturalHeight < 1 ||
        image.naturalWidth > maxSourceDimension || image.naturalHeight > maxSourceDimension
      ) {
        throw new Error("画像の縦横サイズは 12,000px 以下にしてください。");
      }

      cropperContainer.replaceChildren();
      cropper = new CropperConstructor(image, {
        container: cropperContainer,
        template: cropperTemplate,
      });
      dialog.showModal();
    } catch (error) {
      const message = error instanceof Error ? error.message : "画像を開けませんでした。";
      setMessage(clientError, message);
      input.value = "";
      releaseSource();
    }
  });

  applyButton.addEventListener("click", async () => {
    const selection = cropper && cropper.getCropperSelection();
    if (!selection || selection.width <= 0 || selection.height <= 0) {
      setMessage(cropError, "切り抜く範囲を選択してください。");
      return;
    }

    setMessage(cropError, "");
    setProcessing(true);

    try {
      const { blob, size } = await encodeSelection(selection);
      const dataURL = await blobToDataURL(blob);
      if (typeof dataURL !== "string") {
        throw new Error("画像データを作成できませんでした。");
      }

      photoBase64.value = dataURL;
      photoChanged.value = "1";
      preview.src = dataURL;
      preview.hidden = false;
      if (placeholder) placeholder.hidden = true;

      const kibibytes = Math.max(1, Math.ceil(blob.size / 1024));
      setMessage(clientStatus, `${size} × ${size}px・約${kibibytes} KiB に調整しました。`);
      dialog.close();
      input.value = "";
      releaseSource();
    } catch (error) {
      const message = error instanceof Error ? error.message : "画像を処理できませんでした。";
      setMessage(cropError, message);
    } finally {
      setProcessing(false);
    }
  });

  zoomOutButton?.addEventListener("click", () => cropper?.getCropperImage()?.$zoom(-0.1));
  zoomInButton?.addEventListener("click", () => cropper?.getCropperImage()?.$zoom(0.1));
  resetButton?.addEventListener("click", () => {
    const cropperImage = cropper?.getCropperImage();
    cropperImage?.$resetTransform();
    cropperImage?.$center("cover");
    cropper?.getCropperSelection()?.$reset();
  });

  closeButton?.addEventListener("click", closeDialog);
  cancelButton?.addEventListener("click", closeDialog);
  dialog.addEventListener("cancel", (event) => {
    if (processing) {
      event.preventDefault();
      return;
    }
    input.value = "";
    releaseSource();
    setMessage(cropError, "");
  });

  form.addEventListener("submit", (event) => {
    if (!processing) return;
    event.preventDefault();
    setMessage(clientError, "画像の処理が終わるまでお待ちください。");
  });
});
