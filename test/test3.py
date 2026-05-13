#!/usr/bin/env python3
"""
Claude API 테스트 코드
"""

import os

from anthropic import Anthropic


def test_basic_message():
    """기본 메시지 테스트"""
    client = Anthropic(
        api_key="om-ni-88660f931bcd275d259b44eb0354249f14dbb129f2fb6f9fe8b030136e187f34",
        base_url="http://localhost:8080/opencode-go",
    )
    message = client.messages.create(
        model="deepseek-v4-flash",
        max_tokens=1024,
        messages=[{"role": "user", "content": "안녕하세요. 당신의 이름은 무엇인가요?"}],
    )
    assert message.content[0].text
    print(f"✓ Basic message test passed: {message.content[0].text[:50]}...")


if __name__ == "__main__":
    print("Claude API 테스트 시작...\n")

    try:
        test_basic_message()

        print("\n✅ 모든 테스트 완료!")
    except Exception as e:
        print(f"\n❌ 테스트 실패: {e}")
