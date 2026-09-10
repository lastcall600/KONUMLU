"use client";

import { QRCodeSVG } from "qrcode.react";

export function VerifiedQRCode({ value }: { value: string }) {
  return (
    <div className="verified-qr">
      <QRCodeSVG
        value={value}
        size={192}
        level="M"
        bgColor="#ffffff"
        fgColor="#111111"
        title="Doğrulama QR kodu"
      />
    </div>
  );
}
