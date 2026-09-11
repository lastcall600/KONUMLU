import { ModerationReportDetail } from "./ModerationReportDetail";

type PageProps = {
  params: Promise<{ reportId: string }>;
};

export default async function ModerationReportPage({ params }: PageProps) {
  const { reportId } = await params;
  return <ModerationReportDetail reportId={reportId} />;
}
