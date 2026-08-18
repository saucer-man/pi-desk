import { describe, expect, it } from "vitest";
import { parseBatchAskEnvelope, serializeBatchAskAnswers } from "./batchAsk";

const userPayload = {
  questions: [
    {
      id: "listen_port",
      type: "select",
      question: "服务器对外访问端口使用哪个？",
      options: [
        { label: "8000（推荐）", value: "8000", description: "访问地址为 http://104.168.159.226:8000" },
        { label: "8080", value: "8080", description: "访问地址为 http://104.168.159.226:8080" },
      ],
      allowOther: true,
      placeholder: "也可以填写其他端口",
      prefill: "8000",
    },
    {
      id: "data_mode",
      type: "select",
      question: "是否把本机现有监控规则、运行记录和已配置管理员账号一起迁移？",
      options: [
        { label: "完整迁移现有数据（推荐）", value: "migrate", description: "上传现有配置和数据库" },
        { label: "全新部署", value: "fresh", description: "在服务器重新配置" },
      ],
      allowOther: false,
      placeholder: "",
      prefill: "",
    },
  ],
  review: true,
  type: "select",
  question: "",
  options: [],
};

describe("batch ask envelope", () => {
  it("projects the reported ask_question payload when the transport marker is present", () => {
    const parsed = parseBatchAskEnvelope(JSON.stringify(userPayload), true);

    expect(parsed).toMatchObject({
      review: true,
      questions: [
        {
          id: "listen_port", type: "select", prefill: "8000", allowOther: true,
          options: [
            { label: "8000（推荐）", value: "8000", description: "访问地址为 http://104.168.159.226:8000" },
            { label: "8080", value: "8080", description: "访问地址为 http://104.168.159.226:8080" },
          ],
        },
        { id: "data_mode", type: "select", allowOther: false },
      ],
    });
  });

  it("requires either the envelope key or the trusted placeholder marker", () => {
    expect(parseBatchAskEnvelope(JSON.stringify(userPayload))).toBeUndefined();
    expect(parseBatchAskEnvelope(JSON.stringify({ __piDeckBatchAsk: 1, ...userPayload }))).toBeDefined();
  });

  it("rejects duplicate question ids, duplicate option values, and oversized batches", () => {
    expect(parseBatchAskEnvelope({ __piDeckBatchAsk: 1, questions: [userPayload.questions[0], userPayload.questions[0]] })).toBeUndefined();
    expect(parseBatchAskEnvelope({
      __piDeckBatchAsk: 1,
      questions: [{ ...userPayload.questions[0], options: [{ label: "A", value: "same" }, { label: "B", value: "same" }] }],
    })).toBeUndefined();
    expect(parseBatchAskEnvelope({ __piDeckBatchAsk: 1, questions: Array.from({ length: 33 }, (_, index) => ({ id: `q${index}`, type: "input", question: "Q" })) })).toBeUndefined();
  });

  it("serializes ordered structured answers for the extension protocol", () => {
    const parsed = parseBatchAskEnvelope(userPayload, true)!;
    const result = serializeBatchAskAnswers(parsed.questions, [
      { id: "listen_port", type: "select", value: "8000", label: "8000（推荐）" },
      { id: "data_mode", type: "select", value: "migrate", label: "完整迁移现有数据（推荐）" },
    ]);

    expect(JSON.parse(result!)).toEqual({ answers: [
      { id: "listen_port", type: "select", value: "8000", label: "8000（推荐）" },
      { id: "data_mode", type: "select", value: "migrate", label: "完整迁移现有数据（推荐）" },
    ] });
  });
});
