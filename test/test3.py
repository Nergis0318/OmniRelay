"""
Claude API 테스트 코드
"""

from anthropic import Anthropic
from anthropic.types import RedactedThinkingBlock, TextBlock, ThinkingBlock


def test_basic_message():
    """기본 메시지 테스트"""
    client = Anthropic(
        api_key="om-ni-5972098551fef7ae8e9660cca08e9eb8ddea14693f19912e0e2e8bb440babb81",
        base_url="http://100.75.75.1:6002/ccode1",
    )
    message = client.messages.create(
        model="claude-haiku-4-5",
        max_tokens=1024,
        messages=[{"role": "user", "content": "안녕하세요. 당신의 이름은 무엇인가요?"}],
    )
    for block in message.content:
        if isinstance(block, ThinkingBlock):
            print(
                f"  [thinking] {block.thinking[:50] if block.thinking else '(redacted)'}..."
            )
        elif isinstance(block, RedactedThinkingBlock):
            print(f"  [redacted_thinking] {block.data[:50] if block.data else ''}...")
        elif isinstance(block, TextBlock):
            text = block.text
            print(f"✓ Basic message test passed: {text[:50]}...")
            return
    assert False, "No TextBlock found in response"


if __name__ == "__main__":
    print("Claude API 테스트 시작...\n")

    try:
        test_basic_message()

        print("\n✅ 모든 테스트 완료!")
    except Exception as e:
        print(f"\n❌ 테스트 실패: {e}")
