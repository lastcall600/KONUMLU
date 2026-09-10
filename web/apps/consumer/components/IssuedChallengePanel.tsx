"use client";

import { VerifiedQRCode } from "@/components/VerifiedQRCode";
import { qrPayloadForChallenge, type IssuedChallenge } from "@/lib/verified";

const TOKEN_NOTICE =
  "Bu jeton kısa ömürlü ve tek kullanımlıktır. Karşı tarafa uygulama dışında iletin. Sayısal OTP değildir.";
const QR_NOTICE =
  "QR kodu, kısa ömürlü tek kullanımlık doğrulama jetonunu içerir. Karşı taraf tarayıp aynı jetonu gönderir. Sayısal OTP değildir.";
const OTP_NOTICE =
  "Bu 6 haneli kod kısa ömürlü ve tek kullanımlıktır. Karşı tarafa uygulama dışında okuyun. Kod sunucuda saklanmaz.";

export function IssuedChallengePanel({
  issued,
  copied,
  onCopy,
}: {
  issued: IssuedChallenge;
  copied: boolean;
  onCopy: () => void;
}) {
  const qrValue = qrPayloadForChallenge(issued);
  const showQr = qrValue !== null;
  const showOtp = issued.method === "otp";

  return (
    <div>
      {showQr && qrValue !== null ? (
        <>
          <p role="status">QR kodu (bir kez üretilir)</p>
          <VerifiedQRCode value={qrValue} />
          <p>{QR_NOTICE}</p>
        </>
      ) : null}
      {showOtp ? (
        <>
          <p role="status">Doğrulama kodu (bir kez gösterilir)</p>
          <p className="verified-otp-code" aria-label="6 haneli doğrulama kodu">
            {issued.token}
          </p>
          <p>{OTP_NOTICE}</p>
        </>
      ) : null}
      {!showQr && !showOtp ? (
        <>
          <p role="status">Jeton (bir kez gösterilir): {issued.token}</p>
          <p>{TOKEN_NOTICE}</p>
        </>
      ) : null}
      {showQr ? (
        <details>
          <summary>Ham jeton (yedek)</summary>
          <p className="verified-qr-token">{issued.token}</p>
        </details>
      ) : null}
      <button type="button" onClick={onCopy}>
        {showOtp ? "Kodu kopyala" : "Jetonu kopyala"}
      </button>
      {copied ? <p>Kopyalandı.</p> : null}
    </div>
  );
}
